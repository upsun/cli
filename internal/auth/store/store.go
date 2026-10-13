// Package store saves OAuth 2.0 credentials, one entry per session ID, in the system keychain or in files.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

// Entry holds the credentials for one session.
type Entry struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Expires      int64  `json:"expires,omitempty"` // A Unix timestamp.

	// APIToken is set for a token saved by auth:api-token-login.
	APIToken string `json:"api_token,omitempty"`
}

// Backend is where an entry's secrets are stored.
type Backend string

const (
	BackendKeychain Backend = "keychain"
	BackendFile     Backend = "file"
)

// sessionFile is the content of <dir>/<id>.json. It holds no secrets in keychain mode.
type sessionFile struct {
	Backend Backend `json:"backend"`
	Entry   *Entry  `json:"entry,omitempty"`
	// Account is the keychain account. It is unique to each saved session, so that a write abandoned after a
	// timeout, which can finish later, cannot overwrite the secret of a session saved since. In file mode, it is the
	// account of a first write that timed out, which is deleted with the session in case it finished later.
	Account string `json:"account,omitempty"`
}

// MigrationMarker is the name of the file that records the migration from the legacy CLI's storage.
const MigrationMarker = ".migrated"

const defaultKeychainTimeout = 10 * time.Second

var errKeychainTimeout = errors.New("timed out")

// setSecret and deleteSecret change keychain secrets. They are replaced in tests.
var (
	setSecret    = keyring.Set
	deleteSecret = keyring.Delete
)

// Store saves entries. The backend is chosen when a session is first saved, and recorded in the session file.
type Store struct {
	// Dir is the directory for session files and locks, e.g. ~/.upsun-cli/auth.
	Dir string
	// Service is the keychain service name.
	Service string
	// UseKeychain reports whether a new session should try the keychain. It is only called when a session is first
	// saved, as checking can be slow. It may be nil.
	UseKeychain func() bool
	// KeychainTimeout limits each keychain read, and the first write of a session, which falls back to a file. It
	// defaults to 10s. Other changes are waited for, with a notice after the timeout, so that they cannot finish
	// after the session's lock is released.
	KeychainTimeout time.Duration
	// Stderr receives a notice while waiting for the keychain. It may be nil.
	Stderr io.Writer

	// keychainErr is the first keychain failure, which is returned instead of trying again, as each try could prompt
	// the user, e.g. to unlock the keychain.
	keychainErr   error
	keychainErrMu sync.Mutex
}

// KeychainError is returned when the keychain cannot be used for a session that is stored there.
type KeychainError struct {
	Op  string
	Err error
}

func (e *KeychainError) Error() string {
	return fmt.Sprintf("failed to %s credentials in the keychain: %s\n"+
		"Check that the keychain is unlocked, or log in again to store credentials in a file instead.", e.Op, e.Err)
}

func (e *KeychainError) Unwrap() error { return e.Err }

// Load returns the entry for a session, or nil if there is none.
func (s *Store) Load(id string) (*Entry, error) {
	sf, err := s.readSessionFile(id)
	if err != nil || sf == nil {
		return nil, err
	}
	if sf.Backend != BackendKeychain {
		return sf.Entry, nil
	}
	secret, err := s.keychain(false, func() (string, error) { return keyring.Get(s.Service, sf.Account) })
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, &KeychainError{Op: "load", Err: err}
	}
	var e Entry
	if err := json.Unmarshal([]byte(secret), &e); err != nil {
		return nil, fmt.Errorf("invalid credentials in the keychain: %w", err)
	}
	return &e, nil
}

// Save saves the entry for a session.
func (s *Store) Save(id string, e *Entry) error {
	sf, err := s.readSessionFile(id)
	if err != nil {
		return err
	}
	b, err := json.Marshal(e) //nolint:gosec // the entry is stored in the keychain
	if err != nil {
		return err
	}
	if sf != nil && sf.Backend == BackendKeychain {
		if _, err := s.keychain(true, func() (string, error) {
			return "", keyring.Set(s.Service, sf.Account, string(b))
		}); err != nil {
			return &KeychainError{Op: "save", Err: err}
		}
		return nil
	}
	newFile := &sessionFile{Backend: BackendFile, Entry: e}
	if sf != nil {
		newFile.Account = sf.Account
	}
	if sf == nil && s.UseKeychain != nil && s.UseKeychain() {
		// The backend is chosen once, so any keychain failure here (including data that is too big, or a timeout)
		// falls back to a file.
		account := id + "-" + randomSuffix()
		set, del := setSecret, deleteSecret
		_, err := s.keychainCall(false, func() (string, error) {
			return "", set(s.Service, account, string(b))
		}, func() { _ = del(s.Service, account) })
		if err == nil {
			return s.writeSessionFile(id, &sessionFile{Backend: BackendKeychain, Account: account})
		}
		if errors.Is(err, errKeychainTimeout) {
			newFile.Account = account
		}
	}
	return s.writeSessionFile(id, newFile)
}

// Delete removes the entry for a session. It does nothing if there is none.
func (s *Store) Delete(id string) error {
	sf, err := s.readSessionFile(id)
	if err != nil {
		return err
	}
	if sf == nil {
		return nil
	}
	switch {
	case sf.Backend == BackendKeychain:
		_, err := s.keychain(true, func() (string, error) { return "", keyring.Delete(s.Service, sf.Account) })
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return &KeychainError{Op: "delete", Err: err}
		}
	case sf.Account != "":
		// A first write that timed out may have finished later.
		del := deleteSecret
		s.keychainCleanUp(func() error { return del(s.Service, sf.Account) })
	}
	if err := os.Remove(s.sessionFilePath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// List returns the IDs of all stored sessions, sorted.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() && !strings.HasPrefix(id, ".") {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// DeleteAll removes every session, and all other files except locks and the migration marker.
//
// Lock files are kept, as another process may hold a lock on them, and so are the files of sessions whose secrets
// could not be deleted.
func (s *Store) DeleteAll() error {
	ids, err := s.List()
	if err != nil {
		return err
	}
	var errs []error
	// The files of sessions that could not be deleted are kept, so that deleting their secrets can be retried.
	keep := map[string]bool{MigrationMarker: true}
	for _, id := range ids {
		if err := s.Delete(id); err != nil {
			errs = append(errs, err)
			keep[id+".json"] = true
		}
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range entries {
		if !keep[e.Name()] && !strings.HasSuffix(e.Name(), ".lock") {
			errs = append(errs, os.RemoveAll(filepath.Join(s.Dir, e.Name())))
		}
	}
	return errors.Join(errs...)
}

// Forget removes a session's file without deleting its secrets, e.g. if they are in a keychain that cannot be used.
func (s *Store) Forget(id string) error {
	if err := os.Remove(s.sessionFilePath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// LockPath returns the path of the lock file for a session.
func (s *Store) LockPath(id string) string {
	return filepath.Join(s.Dir, id+".lock")
}

func (s *Store) sessionFilePath(id string) string {
	return filepath.Join(s.Dir, id+".json")
}

func (s *Store) readSessionFile(id string) (*sessionFile, error) {
	b, err := os.ReadFile(s.sessionFilePath(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var sf sessionFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return nil, fmt.Errorf("invalid session file %s: %w", s.sessionFilePath(id), err)
	}
	return &sf, nil
}

func (s *Store) writeSessionFile(id string, sf *sessionFile) error {
	b, err := json.Marshal(sf)
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.sessionFilePath(id), b)
}

// keychain runs a keychain call with a timeout. If wait is set, a notice is printed at the timeout, and the call is
// still waited for. After a failure, other calls fail with the same error.
func (s *Store) keychain(wait bool, fn func() (string, error)) (string, error) {
	return s.keychainCall(wait, fn, nil)
}

// keychainCall is like keychain. If the call is abandoned after the timeout and then succeeds, onLate is run.
func (s *Store) keychainCall(wait bool, fn func() (string, error), onLate func()) (string, error) {
	s.keychainErrMu.Lock()
	defer s.keychainErrMu.Unlock()
	if s.keychainErr != nil {
		return "", s.keychainErr
	}
	v, err := s.keychainCallLocked(wait, fn, onLate)
	// Errors about one entry do not mean that the keychain is unavailable.
	if err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, keyring.ErrSetDataTooBig) {
		s.keychainErr = err
	}
	return v, err
}

// keychainCleanUp runs an optional keychain call, ignoring its result, unless the keychain has already failed. Only
// a timeout is remembered, as it usually means an unlock prompt: other failures do not affect other calls.
func (s *Store) keychainCleanUp(fn func() error) {
	s.keychainErrMu.Lock()
	defer s.keychainErrMu.Unlock()
	if s.keychainErr != nil {
		return
	}
	_, err := s.keychainCallLocked(false, func() (string, error) { return "", fn() }, nil)
	if errors.Is(err, errKeychainTimeout) {
		s.keychainErr = err
	}
}

func (s *Store) keychainCallLocked(wait bool, fn func() (string, error), onLate func()) (string, error) {
	timeout := s.KeychainTimeout
	if timeout == 0 {
		timeout = defaultKeychainTimeout
	}
	type result struct {
		v   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(timeout):
		if !wait {
			if onLate != nil {
				go func() {
					if r := <-ch; r.err == nil {
						onLate()
					}
				}()
			}
			return "", fmt.Errorf("%w after %s", errKeychainTimeout, timeout)
		}
	}
	if s.Stderr != nil {
		fmt.Fprintln(s.Stderr, "Waiting for the keychain. Check whether it needs to be unlocked.")
	}
	r := <-ch
	return r.v, r.err
}

// randomSuffix returns a random string for keychain account names.
func randomSuffix() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WriteFileAtomic writes a file with 0600 permissions via a synced temporary file, creating the directory (0700).
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// On Windows, replacing a file fails while another process has it open, e.g. a reader outside the lock.
	for attempt := 0; ; attempt++ {
		err = os.Rename(tmp, path)
		if err == nil || runtime.GOOS != "windows" || attempt == 40 {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}
