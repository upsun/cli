// Package store saves OAuth 2.0 credentials, one entry per session ID, in the system keychain or in files.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
}

// MigrationMarker is the name of the file that records the migration from the legacy CLI's storage.
const MigrationMarker = ".migrated"

const defaultKeychainTimeout = 10 * time.Second

// Store saves entries. The backend is chosen when a session is first saved, and recorded in the session file.
type Store struct {
	// Dir is the directory for session files and locks, e.g. ~/.upsun-cli/auth.
	Dir string
	// Service is the keychain service name.
	Service string
	// UseKeychain reports whether a new session should try the keychain.
	UseKeychain bool
	// KeychainTimeout limits each keychain call. It defaults to 10s.
	KeychainTimeout time.Duration
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
	secret, err := s.keychain(func() (string, error) { return keyring.Get(s.Service, id) })
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
	backend := BackendFile
	switch {
	case sf != nil:
		backend = sf.Backend
	case s.UseKeychain:
		// The backend is chosen once, so a keychain failure here can fall back to a file.
		if err := s.keychainSet(id, b); err == nil {
			backend = BackendKeychain
		} else if errors.Is(err, keyring.ErrSetDataTooBig) {
			return &KeychainError{Op: "save", Err: err}
		}
	}
	if backend == BackendKeychain {
		if sf != nil {
			if err := s.keychainSet(id, b); err != nil {
				return &KeychainError{Op: "save", Err: err}
			}
		}
		return s.writeSessionFile(id, &sessionFile{Backend: BackendKeychain})
	}
	return s.writeSessionFile(id, &sessionFile{Backend: BackendFile, Entry: e})
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
	if sf.Backend == BackendKeychain {
		_, err := s.keychain(func() (string, error) { return "", keyring.Delete(s.Service, id) })
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return &KeychainError{Op: "delete", Err: err}
		}
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

// DeleteAll removes every session, and all other files except the migration marker.
func (s *Store) DeleteAll() error {
	ids, err := s.List()
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		errs = append(errs, s.Delete(id))
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range entries {
		if e.Name() != MigrationMarker {
			errs = append(errs, os.RemoveAll(filepath.Join(s.Dir, e.Name())))
		}
	}
	return errors.Join(errs...)
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

func (s *Store) keychainSet(id string, secret []byte) error {
	_, err := s.keychain(func() (string, error) { return "", keyring.Set(s.Service, id, string(secret)) })
	return err
}

// keychain runs a keychain call with a timeout.
func (s *Store) keychain(fn func() (string, error)) (string, error) {
	timeout := s.KeychainTimeout
	if timeout == 0 {
		timeout = defaultKeychainTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
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
	case <-ctx.Done():
		return "", fmt.Errorf("timed out after %s", timeout)
	}
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
	return os.Rename(tmp, path)
}
