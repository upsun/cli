package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func TestStore(t *testing.T) {
	cases := []struct {
		name        string
		useKeychain bool
		keyringErr  error
		wantBackend Backend
	}{
		{name: "file", wantBackend: BackendFile},
		{name: "keychain", useKeychain: true, wantBackend: BackendKeychain},
		{name: "keychain unavailable", useKeychain: true, keyringErr: errors.New("locked"), wantBackend: BackendFile},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.keyringErr != nil {
				keyring.MockInitWithError(c.keyringErr)
			} else {
				keyring.MockInit()
			}
			s := &Store{
				Dir:         filepath.Join(t.TempDir(), "auth"),
				Service:     "test-cli-auth",
				UseKeychain: func() bool { return c.useKeychain },
			}

			e, err := s.Load("default")
			require.NoError(t, err)
			assert.Nil(t, e)

			entry := &Entry{AccessToken: "a", RefreshToken: "r", TokenType: "bearer", Expires: 123}
			require.NoError(t, s.Save("default", entry))

			sf, err := s.readSessionFile("default")
			require.NoError(t, err)
			assert.Equal(t, c.wantBackend, sf.Backend)
			if c.wantBackend == BackendKeychain {
				assert.Nil(t, sf.Entry, "the session file must not hold secrets in keychain mode")
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(s.sessionFilePath("default"))
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			}

			loaded, err := s.Load("default")
			require.NoError(t, err)
			assert.Equal(t, entry, loaded)

			entry.AccessToken = "a2"
			require.NoError(t, s.Save("default", entry))
			loaded, err = s.Load("default")
			require.NoError(t, err)
			assert.Equal(t, "a2", loaded.AccessToken)

			require.NoError(t, s.Save("other", &Entry{APIToken: "t"}))
			ids, err := s.List()
			require.NoError(t, err)
			assert.Equal(t, []string{"default", "other"}, ids)

			require.NoError(t, s.Delete("default"))
			loaded, err = s.Load("default")
			require.NoError(t, err)
			assert.Nil(t, loaded)
			require.NoError(t, s.Delete("default"))
		})
	}
}

func TestStore_KeychainFailsLater(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: useKeychain}
	require.NoError(t, s.Save("default", &Entry{AccessToken: "a"}))

	// A session stored in the keychain must not silently move to a file.
	keyring.MockInitWithError(errors.New("locked"))
	err := s.Save("default", &Entry{AccessToken: "b"})
	var kerr *KeychainError
	require.ErrorAs(t, err, &kerr)
	_, err = s.Load("default")
	require.ErrorAs(t, err, &kerr)
}

func TestStore_KeychainTooBig(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrSetDataTooBig)
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: useKeychain}
	require.NoError(t, s.Save("default", &Entry{AccessToken: "a"}))
	sf, err := s.readSessionFile("default")
	require.NoError(t, err)
	assert.Equal(t, BackendFile, sf.Backend)
}

func TestStore_KeychainTimeout(t *testing.T) {
	s := &Store{KeychainTimeout: 10 * time.Millisecond}
	_, err := s.keychain(false, func() (string, error) {
		time.Sleep(time.Second)
		return "", nil
	})
	assert.ErrorContains(t, err, "timed out")
}

func TestStore_KeychainWait(t *testing.T) {
	var stderr strings.Builder
	s := &Store{KeychainTimeout: 10 * time.Millisecond, Stderr: &stderr}
	done := false
	v, err := s.keychain(true, func() (string, error) {
		time.Sleep(100 * time.Millisecond)
		done = true
		return "v", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "v", v)
	assert.True(t, done, "a change must finish before the call returns")
	assert.Contains(t, stderr.String(), "Waiting for the keychain")
}

func TestStore_DeleteAll_KeychainError(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: useKeychain}
	require.NoError(t, s.Save("a", &Entry{AccessToken: "a"}))

	keyring.MockInitWithError(errors.New("locked"))
	assert.Error(t, s.DeleteAll())
	// The session file is kept, so that deleting the secret can be retried.
	assert.FileExists(t, s.sessionFilePath("a"))
}

func TestStore_DeleteAll(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth"}
	require.NoError(t, s.Save("a", &Entry{AccessToken: "a"}))
	require.NoError(t, s.Save("b", &Entry{AccessToken: "b"}))
	require.NoError(t, os.WriteFile(s.LockPath("a"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(s.Dir, MigrationMarker), nil, 0o600))

	require.NoError(t, s.DeleteAll())
	entries, err := os.ReadDir(s.Dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{MigrationMarker, "a.lock"}, names)
}

func TestStore_Forget(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: useKeychain}
	require.NoError(t, s.Save("default", &Entry{AccessToken: "a"}))

	// A session whose keychain is unusable can be replaced, e.g. with a file.
	keyring.MockInitWithError(errors.New("locked"))
	require.NoError(t, s.Forget("default"))
	require.NoError(t, s.Save("default", &Entry{AccessToken: "b"}))
	sf, err := s.readSessionFile("default")
	require.NoError(t, err)
	assert.Equal(t, BackendFile, sf.Backend)
}

func useKeychain() bool { return true }

func TestStore_UseKeychainOnlyOnFirstSave(t *testing.T) {
	calls := 0
	s := &Store{Dir: t.TempDir(), UseKeychain: func() bool { calls++; return false }}
	_, err := s.Load("default")
	require.NoError(t, err)
	require.NoError(t, s.Save("default", &Entry{AccessToken: "a"}))
	require.NoError(t, s.Save("default", &Entry{AccessToken: "b"}))
	_, err = s.Load("default")
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

// TestStore_KeychainFailureRemembered checks that a failed keychain call is not repeated, e.g. after a dismissed
// unlock prompt, which would otherwise prompt again.
func TestStore_KeychainFailureRemembered(t *testing.T) {
	s := &Store{}
	calls := 0
	fail := func() (string, error) {
		calls++
		return "", errors.New("dismissed")
	}
	_, err := s.keychain(false, fail)
	require.Error(t, err)
	_, err = s.keychain(false, fail)
	assert.ErrorContains(t, err, "dismissed")
	assert.Equal(t, 1, calls)

	// Errors about one entry are not failures of the keychain.
	for _, entryErr := range []error{keyring.ErrNotFound, keyring.ErrSetDataTooBig} {
		s = &Store{}
		calls = 0
		fn := func() (string, error) {
			calls++
			return "", entryErr
		}
		_, _ = s.keychain(false, fn)
		_, _ = s.keychain(false, fn)
		assert.Equal(t, 2, calls, "error: %v", entryErr)
	}
}

// TestStore_FirstSaveAfterKeychainFailure checks that a new session uses a file without trying the keychain again.
func TestStore_FirstSaveAfterKeychainFailure(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: useKeychain}
	require.NoError(t, s.Save("old", &Entry{AccessToken: "a"}))

	keyring.MockInitWithError(errors.New("dismissed"))
	_, err := s.Load("old")
	require.Error(t, err)

	keyring.MockInit()
	require.NoError(t, s.Save("new", &Entry{AccessToken: "b"}))
	sf, err := s.readSessionFile("new")
	require.NoError(t, err)
	assert.Equal(t, BackendFile, sf.Backend, "the keychain must not be tried again in this process")
}

// TestStore_KeychainAccountPerSession checks that each saved session uses its own keychain account, so that a write
// abandoned by another process, e.g. after a timeout, cannot overwrite it.
func TestStore_KeychainAccountPerSession(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: useKeychain}
	require.NoError(t, s.Save("default", &Entry{AccessToken: "a"}))
	sf, err := s.readSessionFile("default")
	require.NoError(t, err)
	require.Equal(t, BackendKeychain, sf.Backend)
	assert.NotEqual(t, "default", sf.Account)

	// A late write for the session ID, by an earlier attempt, does not change the session.
	require.NoError(t, keyring.Set("test-cli-auth", "default", `{"access_token": "stale"}`))
	e, err := s.Load("default")
	require.NoError(t, err)
	assert.Equal(t, "a", e.AccessToken)

	require.NoError(t, s.Delete("default"))
	_, err = keyring.Get("test-cli-auth", sf.Account)
	assert.ErrorIs(t, err, keyring.ErrNotFound)
}

// TestStore_FirstSaveTimeout checks that a timed-out first write falls back to a file, and that a late write is
// deleted, either by the process or when the session is deleted.
func TestStore_FirstSaveTimeout(t *testing.T) {
	keyring.MockInit()
	deleted := make(chan string, 1)
	origSet, origDelete := setSecret, deleteSecret
	setSecret = func(service, user, password string) error {
		time.Sleep(100 * time.Millisecond)
		return keyring.Set(service, user, password)
	}
	deleteSecret = func(service, user string) error {
		err := keyring.Delete(service, user)
		deleted <- user
		return err
	}
	t.Cleanup(func() { setSecret, deleteSecret = origSet, origDelete })

	s := &Store{
		Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: useKeychain, KeychainTimeout: 10 * time.Millisecond,
	}
	require.NoError(t, s.Save("default", &Entry{AccessToken: "a"}))
	sf, err := s.readSessionFile("default")
	require.NoError(t, err)
	assert.Equal(t, BackendFile, sf.Backend)
	require.NotEmpty(t, sf.Account, "the account must be recorded, to delete a late write")

	// The process deletes the late write.
	select {
	case account := <-deleted:
		assert.Equal(t, sf.Account, account)
	case <-time.After(time.Second):
		t.Fatal("the late write was not deleted")
	}
	_, err = keyring.Get("test-cli-auth", sf.Account)
	assert.ErrorIs(t, err, keyring.ErrNotFound)

	// If the process ended first, deleting the session deletes the late write.
	require.NoError(t, keyring.Set("test-cli-auth", sf.Account, "late"))
	s2 := &Store{Dir: s.Dir, Service: "test-cli-auth"}
	require.NoError(t, s2.Save("default", &Entry{AccessToken: "b"}))
	require.NoError(t, s2.Delete("default"))
	_, err = keyring.Get("test-cli-auth", sf.Account)
	assert.ErrorIs(t, err, keyring.ErrNotFound)
}

// TestStore_CleanupFailureNotRemembered checks that a failed clean-up of a late write does not affect other sessions.
func TestStore_CleanupFailureNotRemembered(t *testing.T) {
	keyring.MockInit()
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth"}
	require.NoError(t, s.writeSessionFile("a", &sessionFile{Backend: BackendFile, Entry: &Entry{}, Account: "a-x"}))

	keyring.MockInitWithError(errors.New("locked"))
	require.NoError(t, s.Delete("a"))
	assert.NoError(t, s.keychainErr)
}

// TestStore_CleanupTimeoutRemembered checks that a clean-up that times out, e.g. at an unlock prompt, stops other
// keychain calls.
func TestStore_CleanupTimeoutRemembered(t *testing.T) {
	keyring.MockInit()
	origDelete := deleteSecret
	deleteSecret = func(_, _ string) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	}
	t.Cleanup(func() { deleteSecret = origDelete })
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", KeychainTimeout: 10 * time.Millisecond}
	require.NoError(t, s.writeSessionFile("a", &sessionFile{Backend: BackendFile, Entry: &Entry{}, Account: "a-x"}))

	require.NoError(t, s.Delete("a"))
	assert.ErrorIs(t, s.keychainErr, errKeychainTimeout)
}
