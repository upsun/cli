// internal/session/manager_test.go
package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

func TestFileStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess-default", "sess-default.json")

	fs := &session.FileStore{}
	s := &session.Session{
		AccessToken:  "tok",
		TokenType:    "bearer",
		Expires:      9999999999,
		RefreshToken: "ref",
	}
	require.NoError(t, fs.Save(path, s))

	loaded, err := fs.Load(path)
	require.NoError(t, err)
	assert.Equal(t, s, loaded)
}

func TestFileStore_LoadMissing(t *testing.T) {
	fs := &session.FileStore{}
	loaded, err := fs.Load("/nonexistent/path.json")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestFileStore_Delete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess-default", "sess-default.json")
	fs := &session.FileStore{}
	require.NoError(t, fs.Save(path, &session.Session{AccessToken: "tok"}))
	require.NoError(t, fs.Delete(filepath.Join(dir, "sess-default")))
	_, err := os.Stat(filepath.Join(dir, "sess-default"))
	assert.True(t, os.IsNotExist(err))
}

func TestFileStore_List(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"sess-cli-default", "sess-cli-work", "sess-default", "other-dir"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0700))
	}
	fs := &session.FileStore{}
	ids, err := fs.List(dir)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"default", "work"}, ids)
}

func TestResolveSessionID_EnvVar(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("TEST_CLI_SESSION_ID", "my-env-session")
	id, err := session.ResolveSessionID(cfg)
	require.NoError(t, err)
	assert.Equal(t, "my-env-session", id)
}

func TestResolveSessionID_File(t *testing.T) {
	cfg := testConfig(t)
	dir := t.TempDir()
	t.Setenv("TEST_CLI_HOME", dir)
	idFile := filepath.Join(dir, ".platform-test-cli", "session-id")
	require.NoError(t, os.MkdirAll(filepath.Dir(idFile), 0700))
	require.NoError(t, os.WriteFile(idFile, []byte("file-session\n"), 0600))
	id, err := session.ResolveSessionID(cfg)
	require.NoError(t, err)
	assert.Equal(t, "file-session", id)
}

func TestResolveSessionID_Default(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("TEST_CLI_HOME", t.TempDir())
	id, err := session.ResolveSessionID(cfg)
	require.NoError(t, err)
	assert.Equal(t, "default", id)
}

// testConfig returns a minimal *config.Config for tests using the integration test config.yaml.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile("../../integration-tests/config.yaml")
	require.NoError(t, err)
	cfg, err := config.FromYAML(data)
	require.NoError(t, err)
	return cfg
}
