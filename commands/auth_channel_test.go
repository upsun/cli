package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"

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

// TestPrepareAuthChannel checks that a child process can send requests on the inherited descriptor, and that it does
// not inherit the token variables, or the channel variables of a parent wrapper.
func TestPrepareAuthChannel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("see TestWindowsAuthChannel")
	}
	t.Setenv("TEST_HOME", t.TempDir())
	t.Setenv("TEST_API_DISABLE_CREDENTIAL_HELPERS", "1")
	t.Setenv("TEST_API_ACCESS_TOKEN", "raw-token")
	t.Setenv("TEST_API_TOKEN", "raw-token")

	cmd := exec.Command("sh", "-c", `printf '%s\n' "$REQUEST" >&"$TEST_WRAPPER_AUTH_FD"
head -n 1 <&"$TEST_WRAPPER_AUTH_FD"
echo "$TEST_WRAPPER_PID $TEST_WRAPPER_HAS_TOKEN" "${TEST_API_ACCESS_TOKEN-unset}" \
	"${TEST_TOKEN-unset}" "${TEST_API_TOKEN-unset}" "${TEST_WRAPPER_AUTH_PIPE-unset}"`)
	cmd.Env = append(os.Environ(), `REQUEST={"command": "token", "session_id": "default"}`,
		"TEST_WRAPPER_AUTH_PIPE=parent")
	var out bytes.Buffer
	cmd.Stdout = &out
	started, done, err := prepareAuthChannel(testConfig(), io.Discard)(cmd)
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	started()
	err = cmd.Wait()
	done()
	require.NoError(t, err)
	assert.Equal(t,
		`{"result":{"access_token":"raw-token"}}`+"\n"+strconv.Itoa(os.Getpid())+" 1 unset unset unset unset\n",
		out.String())
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
