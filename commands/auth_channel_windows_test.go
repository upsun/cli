package commands

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// TestWindowsAuthChannel checks that a child process can send requests on the named pipe, that it does not inherit the
// token variables, and that other processes cannot use the pipe.
func TestWindowsAuthChannel(t *testing.T) {
	if os.Getenv("TEST_AUTH_CHANNEL_CHILD") == "1" {
		authChannelChild(t)
		return
	}
	t.Setenv("TEST_HOME", t.TempDir())
	t.Setenv("TEST_API_DISABLE_CREDENTIAL_HELPERS", "1")
	t.Setenv("TEST_API_ACCESS_TOKEN", "raw-token")
	t.Setenv("TEST_API_TOKEN", "raw-token")

	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsAuthChannel$") //nolint:gosec
	cmd.Env = append(os.Environ(), "TEST_AUTH_CHANNEL_CHILD=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	started, done, err := prepareAuthChannel(testConfig(), io.Discard)(cmd)
	require.NoError(t, err)
	defer done()
	var name string
	for _, e := range cmd.Env {
		if v, ok := strings.CutPrefix(e, "TEST_WRAPPER_AUTH_PIPE="); ok {
			name = v
		}
	}
	require.NotEmpty(t, name)
	path := `\\.\pipe\` + name

	// A connection from another process (here, this one) is closed.
	other, err := winio.DialPipe(path, nil)
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
	assert.Contains(t, out.String(), `{"result":{"access_token":"raw-token"}}`+"\n1 unset unset unset\n")

	// Once the child has connected, the pipe accepts no other connections.
	timeout := time.Second
	_, err = winio.DialPipe(path, &timeout)
	assert.Error(t, err)
}

// authChannelChild sends a request as the legacy CLI does, and prints the response and the relevant variables.
func authChannelChild(t *testing.T) {
	conn, err := winio.DialPipe(`\\.\pipe\`+os.Getenv("TEST_WRAPPER_AUTH_PIPE"), nil)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte(`{"command": "token", "session_id": "default"}` + "\n"))
	require.NoError(t, err)
	line, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	env := func(name string) string {
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return "unset"
	}
	fmt.Print(line)
	fmt.Println(env("TEST_WRAPPER_HAS_TOKEN"), env("TEST_API_ACCESS_TOKEN"), env("TEST_TOKEN"), env("TEST_API_TOKEN"))
}

func TestWindowsPipeSDDL(t *testing.T) {
	sid, err := windows.StringToSid("S-1-5-21-1-2-3-1001")
	require.NoError(t, err)
	sddl := pipeSDDL(sid)
	assert.Equal(t, "O:S-1-5-21-1-2-3-1001D:P(A;;GA;;;S-1-5-21-1-2-3-1001)", sddl)
	_, err = windows.SecurityDescriptorFromString(sddl)
	assert.NoError(t, err)
}
