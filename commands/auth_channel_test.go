package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/auth"
)

func TestAuthChannelServer(t *testing.T) {
	t.Setenv("TEST_HOME", t.TempDir())
	t.Setenv("TEST_API_DISABLE_CREDENTIAL_HELPERS", "1")
	s := &authChannelServer{cnf: testConfig(), stderr: io.Discard, managers: map[string]*auth.Manager{}}
	client, server := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.serve(context.Background(), server)
	}()

	r := bufio.NewReader(client)
	request := func(req string) map[string]any {
		_, err := client.Write([]byte(req + "\n"))
		require.NoError(t, err)
		line, err := r.ReadBytes('\n')
		require.NoError(t, err)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(line, &resp))
		return resp
	}

	// Requests are answered in order on the same connection.
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

	// The server stops when the connection is closed.
	require.NoError(t, server.Close())
	<-done
}

// TestPrepareAuthChannel checks that a child process can send requests on the channel, one connection for each, that
// connections from other processes are refused, and that the child does not inherit the token variables, or the
// channel variables of a parent wrapper.
func TestPrepareAuthChannel(t *testing.T) {
	if os.Getenv("TEST_AUTH_CHANNEL_CHILD") == "1" {
		authChannelChild(t)
		return
	}
	t.Setenv("TEST_HOME", t.TempDir())
	t.Setenv("TEST_API_DISABLE_CREDENTIAL_HELPERS", "1")
	t.Setenv("TEST_API_ACCESS_TOKEN", "raw-token")
	t.Setenv("TEST_API_TOKEN", "raw-token")

	cmd := exec.Command(os.Args[0], "-test.run=^TestPrepareAuthChannel$") //nolint:gosec
	cmd.Env = append(os.Environ(), "TEST_AUTH_CHANNEL_CHILD=1", "TEST_WRAPPER_AUTH_SOCKET=parent",
		"TEST_WRAPPER_AUTH_PIPE=parent")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	started, done, err := prepareAuthChannel(testConfig(), io.Discard)(cmd)
	require.NoError(t, err)
	defer done()
	env := map[string]string{}
	for _, e := range cmd.Env {
		name, value, _ := strings.Cut(e, "=")
		env[name] = value
	}

	// A connection from another process (here, this one) is closed.
	other, err := dialAuthChannel(func(name string) string { return env[name] })
	require.NoError(t, err)
	defer other.Close()
	require.NoError(t, cmd.Start())
	started()
	require.NoError(t, other.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, err = other.Read(make([]byte, 1))
	var netErr net.Error
	assert.Error(t, err)
	assert.False(t, errors.As(err, &netErr) && netErr.Timeout(), "the connection must be closed: %v", err)

	require.NoError(t, cmd.Wait(), "output: %s", out.String())
	token := `{"result":{"access_token":"raw-token"}}` + "\n"
	assert.Contains(t, out.String(), token+token+strconv.Itoa(os.Getpid())+" 1 unset unset unset\n")

	done()
	if runtime.GOOS != "windows" {
		_, err := os.Stat(filepath.Dir(env["TEST_WRAPPER_AUTH_SOCKET"]))
		assert.ErrorIs(t, err, fs.ErrNotExist, "the socket's directory must be removed")
	}
}

// authChannelChild sends two requests as the legacy CLI does, and prints the responses and the relevant variables.
func authChannelChild(t *testing.T) {
	for range 2 {
		conn, err := dialAuthChannel(os.Getenv)
		require.NoError(t, err)
		_, err = conn.Write([]byte(`{"command": "token", "session_id": "default"}` + "\n"))
		require.NoError(t, err)
		line, err := bufio.NewReader(conn).ReadString('\n')
		require.NoError(t, err)
		require.NoError(t, conn.Close())
		fmt.Print(line)
	}
	env := func(name string) string {
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return "unset"
	}
	fmt.Println(env("TEST_WRAPPER_PID"), env("TEST_WRAPPER_HAS_TOKEN"), env("TEST_API_ACCESS_TOKEN"), env("TEST_TOKEN"),
		env("TEST_API_TOKEN"))
}

func TestWithoutEnv(t *testing.T) {
	env := []string{"TEST_TOKEN=a", "TEST_API_TOKEN=b", "TEST_API_ACCESS_TOKEN=c", "TEST_TOKEN_OTHER=d", "OTHER_TOKEN=e",
		"TEST_SESSION_ID=f", "test_Token=g"}
	want := []string{"TEST_TOKEN_OTHER=d", "OTHER_TOKEN=e", "TEST_SESSION_ID=f"}
	if runtime.GOOS != "windows" {
		want = append(want, "test_Token=g")
	}
	assert.Equal(t, want, withoutEnv(env, "TEST_TOKEN", "TEST_API_TOKEN", "TEST_API_ACCESS_TOKEN"))
	assert.Len(t, env, 7, "the input must not be changed")
}
