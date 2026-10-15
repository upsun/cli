package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

// setUpTunnelProject serves an app over SSH, with relationships whose hosts the mock
// SSH server forwards to local services. Each service writes its name to a connection.
func setUpTunnelProject(t *testing.T) *testProject {
	t.Helper()
	p := setUpProject(t, sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Mounts: map[string]mockapi.Mount{}},
		},
	}))
	services := map[string]string{} // host:port → local address
	for _, name := range []string{"database.internal:3306", "redis.internal:6379"} {
		services[name] = serveName(t, strings.Split(name, ".")[0])
	}
	sshServer := p.serveSSH(t, mockssh.ExecHandler(t.TempDir(), remoteEnv(t, nil, mockRelationships())))
	sshServer.DialTCP = func(host string, port int) (net.Conn, error) {
		addr, ok := services[net.JoinHostPort(host, strconv.Itoa(port))]
		if !ok {
			return nil, fmt.Errorf("unknown service: %s:%d", host, port)
		}
		return net.Dial("tcp", addr)
	}
	return p
}

// serveName starts a TCP server that writes a name to each connection, returning its address.
func serveName(t *testing.T, name string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(c, name)
			_ = c.Close()
		}
	}()
	return l.Addr().String()
}

// readTunnel connects to a local port, retrying until the tunnel is ready, and returns what the service wrote.
func readTunnel(t *testing.T, port int) string {
	t.Helper()
	var out string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		require.NoError(c, err)
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		b, err := io.ReadAll(conn)
		require.NoError(c, err)
		require.NotEmpty(c, b)
		out = string(b)
	}, 10*time.Second, 100*time.Millisecond)
	return out
}

// runWithFiles runs a command with its output written to files instead of pipes, as background
// processes that it starts inherit them, and waiting for a pipe would wait for those processes.
func runWithFiles(f *cmdFactory, args ...string) (stdOut, stdErr string, err error) {
	f.t.Helper()
	dir := f.t.TempDir()
	stdOutFile, err := os.Create(filepath.Join(dir, "stdout"))
	require.NoError(f.t, err)
	defer stdOutFile.Close()
	stdErrFile, err := os.Create(filepath.Join(dir, "stderr"))
	require.NoError(f.t, err)
	defer stdErrFile.Close()
	cmd := f.buildCommand(args...)
	cmd.Stdout = stdOutFile
	cmd.Stderr = stdErrFile
	f.t.Log("Running:", cmd)
	err = cmd.Run()
	stdOut, stdErr = readFile(f.t, stdOutFile.Name()), readFile(f.t, stdErrFile.Name())
	f.logStderr(stdErr)
	return stdOut, stdErr, err
}

// freePort returns a local port which is not in use.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return tcpPort(t, l)
}

// tcpPort returns the port of a TCP listener.
func tcpPort(t *testing.T, l net.Listener) int {
	t.Helper()
	addr, ok := l.Addr().(*net.TCPAddr)
	require.True(t, ok)
	return addr.Port
}

func TestTunnelOpen(t *testing.T) {
	t.Parallel()
	p := setUpTunnelProject(t)
	f := p.f
	args := []string{"-p", layoutProjectID, "-e", "main"}
	// The tunnels run in the background until they are closed, or the SSH server stops.
	t.Cleanup(func() { _, _, _ = f.RunCombinedOutput("tunnel:close", "--all", "--yes") })

	_, stdErr, err := runWithFiles(f, append([]string{"tunnel:open"}, args...)...)
	require.NoError(t, err)
	assert.Contains(t, stdErr, "SSH tunnel opened to database at: mysql://user:@127.0.0.1:")
	assert.Contains(t, stdErr, "SSH tunnel opened to redis at: redis://:@127.0.0.1:")

	var relationships map[string][]struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	encoded := strings.TrimSpace(f.Run(append([]string{"tunnel:info", "--encode"}, args...)...))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(decoded, &relationships))
	require.Len(t, relationships["database"], 1)
	require.Len(t, relationships["redis"], 1)
	assert.Equal(t, "127.0.0.1", relationships["database"][0].Host)

	dbPort := relationships["database"][0].Port
	redisPort := relationships["redis"][0].Port
	assert.Equal(t, "database", readTunnel(t, dbPort))
	assert.Equal(t, "redis", readTunnel(t, redisPort))

	assertTrimmed(t, strconv.Itoa(dbPort), f.Run(append([]string{"tunnel:info", "-P", "database.0.port"}, args...)...))

	list := f.Run(append([]string{"tunnel:list", "--format", "csv", "--columns", "port,relationship"}, args...)...)
	assert.Equal(t, fmt.Sprintf("Port,Relationship\n%d,database\n%d,redis\n", dbPort, redisPort), list)

	// Opening tunnels again reuses them.
	_, stdErr, err = runWithFiles(f, append([]string{"tunnel:open"}, args...)...)
	require.NoError(t, err)
	assert.Contains(t, stdErr, fmt.Sprintf(
		"A tunnel is already opened to the relationship database, at: mysql://user:@127.0.0.1:%d", dbPort))

	_, stdErr, err = f.RunCombinedOutput(append([]string{"tunnel:close", "--yes"}, args...)...)
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Close tunnel to relationship database on "+layoutProjectID+"-main--app?")
	assert.Contains(t, stdErr, "Close tunnel to relationship redis on "+layoutProjectID+"-main--app?")

	// The tunnel processes exit.
	assert.Eventually(t, func() bool {
		conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(dbPort)))
		if err == nil {
			_ = conn.Close()
		}
		return err != nil
	}, 10*time.Second, 100*time.Millisecond)

	_, stdErr, err = f.RunCombinedOutput(append([]string{"tunnel:list"}, args...)...)
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "No tunnels found.")
}

func TestTunnelSingle(t *testing.T) {
	t.Parallel()
	p := setUpTunnelProject(t)
	f := p.f
	port := freePort(t)

	cmd := f.buildCommand("tunnel:single", "-p", layoutProjectID, "-e", "main", "-r", "redis",
		"--port", strconv.Itoa(port))
	var stdErr bytes.Buffer
	cmd.Stderr = &stdErr
	require.NoError(t, cmd.Start())
	var waitErr error
	exited := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			t.Error("tunnel:single did not exit")
		}
		f.logStderr(stdErr.String())
	})

	assert.Equal(t, "redis", readTunnel(t, port))
	assertTrimmed(t, strconv.Itoa(port), f.Run("tunnel:info", "-p", layoutProjectID, "-e", "main",
		"-P", "redis.0.port"))

	// Closing the tunnel ends the command.
	f.Run("tunnel:close", "--all", "--yes")
	select {
	case <-exited:
		// The command reports the stopped process, but it exits successfully.
		assert.NoError(t, waitErr)
	case <-time.After(10 * time.Second):
		t.Fatal("tunnel:single did not exit after tunnel:close")
	}
	assert.Contains(t, stdErr.String(), fmt.Sprintf("SSH tunnel opened to redis at: redis://:@127.0.0.1:%d", port))
}
