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
	if !authChannelSupported {
		t.Skip("the auth channel is not supported on " + runtime.GOOS)
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
	env := envMap(cmd.Env)

	// A connection from another process (here, this one) is refused, and closed.
	other, err := dialAuthChannel(func(name string) string { return env[name] })
	require.NoError(t, err)
	defer other.Close()
	require.NoError(t, cmd.Start())
	started()
	require.NoError(t, other.SetDeadline(time.Now().Add(10*time.Second)))
	_, err = other.Write([]byte(`{"command": "token", "session_id": "default"}` + "\n"))
	require.NoError(t, err)
	b, err := io.ReadAll(other)
	var netErr net.Error
	assert.False(t, errors.As(err, &netErr) && netErr.Timeout(), "the connection must be closed: %v", err)
	assert.JSONEq(t, `{"refused": true}`, string(b))

	// A connection that sends nothing is refused after a timeout.
	silent, err := dialAuthChannel(func(name string) string { return env[name] })
	require.NoError(t, err)
	defer silent.Close()
	require.NoError(t, silent.SetDeadline(time.Now().Add(10*time.Second)))
	b, err = io.ReadAll(silent)
	assert.False(t, errors.As(err, &netErr) && netErr.Timeout(), "the connection must be closed: %v", err)
	assert.JSONEq(t, `{"refused": true}`, string(b))

	require.NoError(t, cmd.Wait(), "output: %s", out.String())
	token := `{"result":{"access_token":"raw-token"}}` + "\n"
	assert.Contains(t, out.String(), token+token+strconv.Itoa(os.Getpid())+" 1 unset unset unset\n")

	done()
	if runtime.GOOS != "windows" {
		dir := filepath.Dir(env["TEST_WRAPPER_AUTH_SOCKET"])
		assert.True(t, strings.HasPrefix(filepath.Base(dir), "cli-auth-"+strconv.Itoa(os.Getpid())+"-"),
			"the socket's directory must be named after this process: %s", dir)
		_, err := os.Stat(dir)
		assert.ErrorIs(t, err, fs.ErrNotExist, "the socket's directory must be removed")
	}
}

// TestPrepareAuthChannel_Unsupported checks that where the channel is not supported, the child's environment is left
// as it is, so that the legacy CLI runs auth:internal with the same credentials.
func TestPrepareAuthChannel_Unsupported(t *testing.T) {
	if authChannelSupported {
		t.Skip("the auth channel is supported on " + runtime.GOOS)
	}
	t.Setenv("TEST_HOME", t.TempDir())
	t.Setenv("TEST_API_DISABLE_CREDENTIAL_HELPERS", "1")
	env := []string{"TEST_API_TOKEN=raw-token", "OTHER=1"}
	cmd := &exec.Cmd{Env: env}
	started, done, err := prepareAuthChannel(testConfig(), io.Discard)(cmd)
	require.NoError(t, err)
	started()
	done()
	assert.Equal(t, env, cmd.Env)
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

// TestServeListener_AcceptError checks that the channel keeps listening after an error from Accept.
func TestServeListener_AcceptError(t *testing.T) {
	if !authChannelSupported {
		t.Skip("the auth channel is not supported on " + runtime.GOOS)
	}
	t.Setenv("TEST_HOME", t.TempDir())
	t.Setenv("TEST_API_DISABLE_CREDENTIAL_HELPERS", "1")
	cmd := &exec.Cmd{}
	l, err := listenAuthChannel(testConfig(), cmd)
	require.NoError(t, err)
	s := &authChannelServer{cnf: testConfig(), stderr: io.Discard, managers: map[string]*auth.Manager{}}
	ctx, cancel := context.WithCancel(context.Background())
	pid := make(chan int, 1)
	pid <- os.Getpid()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.serveListener(ctx, &failOnceListener{Listener: l}, pid)
	}()
	defer func() {
		cancel()
		_ = l.Close()
		<-done
	}()
	env := envMap(cmd.Env)

	conn, err := dialAuthChannel(func(name string) string { return env[name] })
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte(`{"command": "other", "session_id": "default"}` + "\n"))
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	line, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	assert.JSONEq(t, `{"error": "unknown command: other"}`, line)
}

// envMap returns environment variables by name.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		name, value, _ := strings.Cut(e, "=")
		m[name] = value
	}
	return m
}

type failOnceListener struct {
	net.Listener
	failed bool
}

func (l *failOnceListener) Accept() (net.Conn, error) {
	if !l.failed {
		l.failed = true
		return nil, errors.New("transient")
	}
	return l.Listener.Accept()
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
