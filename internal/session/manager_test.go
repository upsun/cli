// internal/session/manager_test.go
package session_test

import (
	"os"
	"path/filepath"
	"strings"
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
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o700))
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
	require.NoError(t, os.MkdirAll(filepath.Dir(idFile), 0o700))
	require.NoError(t, os.WriteFile(idFile, []byte("file-session\n"), 0o600))
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

func TestResolveSessionID_ConfigField(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("TEST_CLI_HOME", t.TempDir())
	cfg.API.SessionID = "config-session"
	id, err := session.ResolveSessionID(cfg)
	require.NoError(t, err)
	assert.Equal(t, "config-session", id)
}

func TestManager_SaveAndLoad(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("TEST_CLI_HOME", t.TempDir())
	store := session.NewMemStore()
	mgr := session.NewWithStore(cfg, store)

	s := &session.Session{AccessToken: "tok", TokenType: "bearer", Expires: 9999999999, RefreshToken: "ref"}
	require.NoError(t, mgr.Save(s))

	loaded, err := mgr.Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "tok", loaded.AccessToken)
}

func TestManager_Delete(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("TEST_CLI_HOME", t.TempDir())
	store := session.NewMemStore()
	mgr := session.NewWithStore(cfg, store)

	require.NoError(t, mgr.Save(&session.Session{AccessToken: "tok"}))
	require.NoError(t, mgr.Delete())

	loaded, err := mgr.Load()
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestManager_APIToken(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("TEST_CLI_HOME", t.TempDir())
	store := session.NewMemStore()
	mgr := session.NewWithStore(cfg, store)

	require.NoError(t, mgr.SetAPIToken("my-api-token"))
	tok, err := mgr.GetAPIToken()
	require.NoError(t, err)
	assert.Equal(t, "my-api-token", tok)

	require.NoError(t, mgr.DeleteAPIToken())
	tok, err = mgr.GetAPIToken()
	require.NoError(t, err)
	assert.Equal(t, "", tok)
}

func TestManager_List(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("TEST_CLI_HOME", t.TempDir())
	store := session.NewMemStore()

	mgr1 := session.NewWithStore(cfg, store)
	require.NoError(t, mgr1.Save(&session.Session{AccessToken: "tok1"}))

	// Second session
	cfg2 := testConfig(t)
	cfg2.API.SessionID = "work"
	mgr2 := session.NewWithStore(cfg2, store)
	require.NoError(t, mgr2.Save(&session.Session{AccessToken: "tok2"}))

	ids, err := mgr1.List()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"default", "work"}, ids)
}

func TestManager_SetActiveSessionID(t *testing.T) {
	cfg := testConfig(t)
	dir := t.TempDir()
	t.Setenv("TEST_CLI_HOME", dir)
	store := session.NewMemStore()
	mgr := session.NewWithStore(cfg, store)

	require.NoError(t, mgr.SetActiveSessionID("work"))

	idFile := filepath.Join(dir, ".platform-test-cli", "session-id")
	data, err := os.ReadFile(idFile)
	require.NoError(t, err)
	assert.Equal(t, "work", strings.TrimSpace(string(data)))
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
