package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
			s := &Store{Dir: filepath.Join(t.TempDir(), "auth"), Service: "test-cli-auth", UseKeychain: c.useKeychain}

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
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: true}
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
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: true}
	err := s.Save("default", &Entry{AccessToken: "a"})
	assert.ErrorIs(t, err, keyring.ErrSetDataTooBig)
}

func TestStore_KeychainTimeout(t *testing.T) {
	s := &Store{KeychainTimeout: 10 * time.Millisecond}
	_, err := s.keychain(func() (string, error) {
		time.Sleep(time.Second)
		return "", nil
	})
	assert.ErrorContains(t, err, "timed out")
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
	s := &Store{Dir: t.TempDir(), Service: "test-cli-auth", UseKeychain: true}
	require.NoError(t, s.Save("default", &Entry{AccessToken: "a"}))

	// A session whose keychain is unusable can be replaced, e.g. with a file.
	keyring.MockInitWithError(errors.New("locked"))
	require.NoError(t, s.Forget("default"))
	require.NoError(t, s.Save("default", &Entry{AccessToken: "b"}))
	sf, err := s.readSessionFile("default")
	require.NoError(t, err)
	assert.Equal(t, BackendFile, sf.Backend)
}
