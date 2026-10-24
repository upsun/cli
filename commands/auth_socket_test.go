package commands

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the auth socket is not used on Windows")
	}
	t.Setenv("TEST_HOME", t.TempDir())
	t.Setenv("TEST_API_DISABLE_CREDENTIAL_HELPERS", "1")
	cnf := testConfig()

	env, stop, err := startAuthSocket(cnf, io.Discard)
	require.NoError(t, err)
	require.Len(t, env, 1)
	path, ok := strings.CutPrefix(env[0], "TEST_WRAPPER_AUTH_SOCKET=")
	require.True(t, ok)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSocket, info.Mode().Type())

	request := func(req string) map[string]any {
		conn, err := net.Dial("unix", path)
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.Write([]byte(req + "\n"))
		require.NoError(t, err)
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		require.NoError(t, err)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(line, &resp))
		return resp
	}

	assert.Equal(t, map[string]any{"login_required": map[string]any{}},
		request(`{"command": "token", "session_id": "default"}`))
	assert.Equal(t,
		map[string]any{"result": map[string]any{"logged_in": false, "session_ids": []any{}, "has_stored_api_token": false}},
		request(`{"command": "status", "session_id": "default"}`))
	assert.Equal(t, map[string]any{"error": "invalid session ID: a/b"},
		request(`{"command": "status", "session_id": "a/b"}`))
	assert.Equal(t, map[string]any{"error": "unknown command: other"},
		request(`{"command": "other", "session_id": "default"}`))
	assert.Contains(t, request(`not json`)["error"], "invalid request")

	t.Setenv("TEST_API_ACCESS_TOKEN", "raw-token")
	stop()
	assert.NoFileExists(t, path)

	// A new server reads the config again.
	env, stop, err = startAuthSocket(cnf, io.Discard)
	require.NoError(t, err)
	defer stop()
	path = strings.TrimPrefix(env[0], "TEST_WRAPPER_AUTH_SOCKET=")
	assert.Equal(t, map[string]any{"result": map[string]any{"access_token": "raw-token"}},
		request(`{"command": "token", "session_id": "default"}`))
}

// TestListenAuthSocket_LongTempDir checks that the socket is created in /tmp if the temporary directory path is long.
func TestListenAuthSocket_LongTempDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the auth socket is not used on Windows")
	}
	long := filepath.Join(t.TempDir(), strings.Repeat("a", 100))
	require.NoError(t, os.MkdirAll(long, 0o700))
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", long)

	dir, l, err := listenAuthSocket("test-cli-auth-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	defer l.Close()
	assert.Equal(t, "/tmp", filepath.Dir(dir))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}
