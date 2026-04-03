// internal/session/manager_test.go
package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
