package tests

import (
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

func TestEnvironmentXdebug(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	sshServer, err := mockssh.NewServer(t, authServer.URL+"/ssh/authority")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := sshServer.Stop(); err != nil {
			t.Error(err)
		}
	})

	// Record SSH commands by user, instead of running them. The tunnel itself
	// fails, as the server rejects port forwarding.
	var mu sync.Mutex
	commands := map[string][]string{}
	sshServer.CommandHandler = func(conn ssh.ConnMetadata, command string, _ mockssh.CommandIO) int {
		mu.Lock()
		defer mu.Unlock()
		commands[conn.User()] = append(commands[conn.User()], command)
		return 0
	}

	projectID := mockapi.ProjectID()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "my-user-id"})
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	xdebug := map[string]any{"xdebug": map[string]any{"idekey": "secret_key"}}
	apps := map[string]mockapi.App{
		"php":    {Type: "php:8.4", Runtime: xdebug},
		"no-key": {Type: "php:8.4"},
		"nodejs": {Type: "nodejs:22", Runtime: xdebug},
		"list": {Type: "composable:25.11", Stack: map[string]any{
			"runtimes": []any{"nodejs@22", map[string]any{"php@8.4": xdebug}},
		}},
		"mapping": {Type: "composable:25.11", Stack: map[string]any{"runtimes": map[string]any{"php@8.4": xdebug}}},
		"string":  {Type: "composable:25.11", Stack: map[string]any{"runtimes": "php@8.4"}},
		"stack":   {Type: "composable:25.11", Stack: []any{"nodejs@22", map[string]any{"php@8.4": xdebug}}},
	}
	envs := make([]*mockapi.Environment, 0, len(apps))
	for name, app := range apps {
		app.Name = "app"
		app.Mounts = map[string]mockapi.Mount{}
		env := makeEnv(projectID, name, "development", "active", nil)
		env.SetCurrentDeployment(&mockapi.Deployment{
			WebApps:  map[string]mockapi.App{"app": app},
			Services: map[string]mockapi.Service{},
			Workers: map[string]mockapi.Worker{
				"app--queue": {App: mockapi.App{Name: "app--queue", Type: app.Type, Runtime: app.Runtime, Stack: app.Stack}},
			},
			Routes: mockRoutes(),
			Links:  mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/" + name + "/deployment/current"),
		})
		env.Links["pf:ssh:app"] = mockapi.HALLink{HREF: "ssh://" + name + "--app@ssh.cli-tests.example.com"}
		env.Links["pf:ssh:app--queue"] = mockapi.HALLink{HREF: "ssh://" + name + "--queue@ssh.cli-tests.example.com"}
		envs = append(envs, env)
	}
	apiHandler.SetEnvironments(envs)

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = []string{
		EnvPrefix + "SSH_OPTIONS=HostName 127.0.0.1\nPort " + strconv.Itoa(sshServer.Port()),
		EnvPrefix + "SSH_HOST_KEYS=" + sshServer.HostKeyConfig(),
	}

	cases := []struct {
		env      string
		args     []string
		wantUser string
	}{
		{env: "php", wantUser: "php--app"},
		{env: "php", args: []string{"--worker", "queue"}, wantUser: "php--queue"},
		{env: "no-key"},
		{env: "nodejs"},
		{env: "list", wantUser: "list--app"},
		{env: "list", args: []string{"--worker", "queue"}, wantUser: "list--queue"},
		{env: "mapping", wantUser: "mapping--app"},
		{env: "string"},
		{env: "stack", wantUser: "stack--app"},
	}
	for _, c := range cases {
		args := append([]string{"xdebug", "-p", projectID, "-e", c.env}, c.args...)
		_, stdErr, err := f.RunCombinedOutput(args...)
		if c.wantUser == "" {
			assert.Error(t, err, c.env)
			assert.Contains(t, stdErr, "No IDE key found.", c.env)
			continue
		}
		assert.Contains(t, stdErr, "Opening a local tunnel for Xdebug.", c.env)
		mu.Lock()
		assert.Equal(t, []string{"rm -rf /run/xdebug-tunnel.sock"}, commands[c.wantUser], c.env)
		mu.Unlock()
	}
}

func TestEnvironmentXdebugTunnel(t *testing.T) {
	t.Parallel()
	p := setUpProject(t, sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "php:8.4", Size: "M", Mounts: map[string]mockapi.Mount{},
				Runtime: map[string]any{"xdebug": map[string]any{"idekey": "secret_key"}}},
		},
	}))
	sshServer := p.serveSSH(t, func(_ ssh.ConnMetadata, _ string, _ mockssh.CommandIO) int { return 0 })

	// The server listens at a local socket instead of /run/xdebug-tunnel.sock. It is
	// in /tmp, as socket paths are limited to about 100 characters.
	socketDir, err := os.MkdirTemp("/tmp", "xdebug")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "xdebug.sock")
	var requestedPath atomic.Value
	sshServer.ListenUnix = func(path string) (net.Listener, error) {
		requestedPath.Store(path)
		return net.Listen("unix", socket)
	}

	// The IDE listens locally.
	ide, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ide.Close() })
	port := tcpPort(t, ide)
	received := make(chan string, 1)
	go func() {
		c, err := ide.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		b, _ := io.ReadAll(c)
		received <- string(b)
	}()

	cmd := p.f.buildCommand("xdebug", "-p", layoutProjectID, "-e", "main", "--port", strconv.Itoa(port))
	var stdErr syncBuffer
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
			t.Error("xdebug did not exit")
		}
		p.f.logStderr(stdErr.String())
	})

	// A connection from the app (Xdebug) to the socket reaches the IDE.
	var conn net.Conn
	require.Eventually(t, func() bool {
		conn, err = net.Dial("unix", socket)
		return err == nil
	}, 10*time.Second, 50*time.Millisecond)
	_, err = io.WriteString(conn, "<init idekey=\"secret_key\"/>")
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	select {
	case got := <-received:
		assert.Equal(t, "<init idekey=\"secret_key\"/>", got)
	case <-time.After(10 * time.Second):
		t.Fatal("the IDE did not receive a connection")
	}
	assert.Equal(t, "/run/xdebug-tunnel.sock", requestedPath.Load())
	require.Eventually(t, func() bool {
		return strings.Contains(stdErr.String(), "Xdebug tunnel opened at: 127.0.0.1:"+strconv.Itoa(port))
	}, 10*time.Second, 50*time.Millisecond)

	// The tunnel closes with the SSH connection.
	require.NoError(t, sshServer.Stop())
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("xdebug did not exit after the SSH connection closed")
	}
	assertExitCode(t, 255, waitErr)
}
