package tests

import (
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

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
	}
	envs := make([]*mockapi.Environment, 0, len(apps))
	for name, app := range apps {
		app.Name = "app"
		app.Mounts = map[string]mockapi.Mount{}
		env := makeEnv(projectID, name, "development", "active", nil)
		env.SetCurrentDeployment(&mockapi.Deployment{
			WebApps:  map[string]mockapi.App{"app": app},
			Services: map[string]mockapi.App{},
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
