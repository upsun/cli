package tests

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

func TestSSH(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"

	sshServer, err := mockssh.NewServer(t, authServer.URL+"/ssh/authority")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sshServer.Stop(); err != nil {
			t.Error(err)
		}
	})

	projectID := mockapi.ProjectID()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID: projectID,
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID,
				"environments=/projects/"+projectID+"/environments",
			),
			DefaultBranch: "main",
		},
	})
	mainEnv := makeEnv(projectID, "main", "production", "active", nil)
	mainEnv.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Disk: 2048, Mounts: map[string]mockapi.Mount{}},
		},
		Services: map[string]mockapi.Service{},
		Workers:  map[string]mockapi.Worker{},
		Routes:   mockRoutes(),
		Links:    mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	mainEnv.Links["pf:ssh:app:0"] = mockapi.HALLink{HREF: "ssh://app--0@ssh.cli-tests.example.com"}
	mainEnv.Links["pf:ssh:app:1"] = mockapi.HALLink{HREF: "ssh://app--1@ssh.cli-tests.example.com"}
	apiHandler.SetEnvironments([]*mockapi.Environment{
		mainEnv,
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = []string{
		EnvPrefix + "SSH_OPTIONS=HostName 127.0.0.1\nPort " + strconv.Itoa(sshServer.Port()),
		EnvPrefix + "SSH_HOST_KEYS=" + sshServer.HostKeyConfig(),
	}

	f.Run("cc")
	wd, _ := os.Getwd()
	// The mock server runs commands with bash, which may print mixed separators on Windows.
	assert.Equal(t, wd, filepath.Clean(strings.TrimSuffix(f.Run("ssh", "-p", projectID, "-e", ".", "pwd"), "\n")))

	_, stdErr, err := f.RunCombinedOutput("ssh", "-p", projectID, "-e", "main", "--instance", "2", "pwd")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Available instances: 0, 1")

	_, _, err = f.RunCombinedOutput("ssh", "-p", projectID, "-e", "main", "--instance", "1", "exit 2")
	var exitErr *exec.ExitError
	assert.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 2, exitErr.ExitCode())
}

func TestSSHAppSelection(t *testing.T) {
	t.Parallel()
	p := setUpProject(t, sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app":   {Name: "app", Type: "golang:1.23", Size: "M", Mounts: map[string]mockapi.Mount{}},
			"admin": {Name: "admin", Type: "php:8.4", Size: "M", Mounts: map[string]mockapi.Mount{}},
		},
		Workers: map[string]mockapi.Worker{
			"app--queue": {App: mockapi.App{
				Name: "app--queue", Type: "golang:1.23", Size: "S", Mounts: map[string]mockapi.Mount{},
			}},
		},
	}))
	// The server prints the SSH user, which identifies the container.
	p.serveSSH(t, func(conn ssh.ConnMetadata, command string, io mockssh.CommandIO) int {
		_, _ = fmt.Fprintf(io.StdOut, "%s: %s\n", conn.User(), command)
		return 0
	})
	f := p.f
	sshUser := layoutProjectID + "-main--"
	sshURL := func(name string) string { return sshUser + name + "@ssh.cli-tests.example.com" }

	assert.Equal(t, sshURL("admin"), f.Run("ssh", "-p", layoutProjectID, "-e", "main", "--app", "admin", "--pipe"))
	assert.Equal(t, sshURL("app--queue"),
		f.Run("ssh", "-p", layoutProjectID, "-e", "main", "--worker", "app--queue", "--pipe"))
	assert.Equal(t, sshURL("app--queue"),
		f.Run("ssh", "-p", layoutProjectID, "-e", "main", "--app", "app", "--worker", "queue", "--pipe"))

	all := strings.Split(strings.TrimSpace(f.Run("ssh", "-p", layoutProjectID, "-e", "main", "--all")), "\n")
	assert.ElementsMatch(t, []string{sshURL("app"), sshURL("admin"), sshURL("app--queue")}, all)

	assert.Equal(t, sshUser+"admin: echo hi\n",
		f.Run("ssh", "-p", layoutProjectID, "-e", "main", "-A", "admin", "echo hi"))
	assert.Equal(t, sshUser+"app--queue: ls -a\n",
		f.Run("ssh", "-p", layoutProjectID, "-e", "main", "--worker", "app--queue", "--", "ls", "-a"))

	_, stdErr, err := f.RunCombinedOutput("ssh", "-p", layoutProjectID, "-e", "main", "--app", "missing", "--pipe")
	assert.Error(t, err)
	assert.Contains(t, stdErr, `App not found: missing`)

	_, stdErr, err = f.RunCombinedOutput("ssh", "-p", layoutProjectID, "-e", "main", "--pipe")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Specifying the --app or --worker is required in non-interactive mode")
}

// TestSSHWorkerSelection checks how --worker matches a worker by its short name, and the interactive choice of an app
// or worker.
func TestSSHWorkerSelection(t *testing.T) {
	t.Parallel()
	app := func(name string) mockapi.App {
		return mockapi.App{Name: name, Type: "golang:1.23", Size: "S", Mounts: map[string]mockapi.Mount{}}
	}
	f := setUpProject(t, sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{"app": app("app"), "admin": app("admin")},
		Workers: map[string]mockapi.Worker{
			"app--queue":   {App: app("app--queue")},
			"app--mail":    {App: app("app--mail")},
			"admin--queue": {App: app("admin--queue")},
		},
	})).f
	sshURL := func(container string) string {
		return layoutProjectID + "-main--" + container + "@ssh.cli-tests.example.com"
	}
	sshArgs := func(args ...string) []string {
		return append([]string{"ssh", "-p", layoutProjectID, "-e", "main", "--pipe"}, args...)
	}

	// A short worker name is qualified with the app name, or matched against all apps' workers.
	assert.Equal(t, sshURL("admin--queue"), f.Run(sshArgs("--app", "admin", "--worker", "queue")...))
	assert.Equal(t, sshURL("app--mail"), f.Run(sshArgs("--worker", "mail")...))

	stdOut, stdErr, err := f.RunInteractive("app--queue\n", sshArgs("--worker", "queue")...)
	require.NoError(t, err, stdErr)
	assert.Equal(t, sshURL("app--queue"), stdOut)
	assert.Contains(t, stdErr, "Enter a number to choose a worker:")

	stdOut, stdErr, err = f.RunInteractive("app--mail\n", sshArgs()...)
	require.NoError(t, err, stdErr)
	assert.Equal(t, sshURL("app--mail"), stdOut)
	assert.Contains(t, stdErr, "Enter a number to choose an app or a worker:")

	cases := []struct {
		args    []string
		wantErr string
	}{
		{[]string{"--worker", "queue"}, "Ambiguous worker name: queue (matches: "},
		{[]string{"--worker", "missing"}, "Worker not found: missing"},
		{[]string{"--app", "admin", "--worker", "mail"}, "Worker not found: mail (in app: admin)"},
		{[]string{"--app", "admin", "--worker", "app--queue"}, `App name "admin" conflicts with worker name "app--queue"`},
	}
	for _, c := range cases {
		_, stdErr, err := f.RunCombinedOutput(sshArgs(c.args...)...)
		assert.Error(t, err, c.args)
		assert.Contains(t, stdErr, c.wantErr, c.args)
	}
}

func TestSSHInactiveEnvironment(t *testing.T) {
	t.Parallel()
	// Paused and inactive environments have no current deployment.
	statuses := []string{"paused", "inactive"}
	envs := make([]*mockapi.Environment, 0, len(statuses))
	for _, status := range statuses {
		env := makeEnv(layoutProjectID, status, "development", status, "main")
		env.Links["pf:ssh:app"] = mockapi.HALLink{HREF: "ssh://" + status + "--app@ssh.cli-tests.example.com"}
		envs = append(envs, env)
	}
	f := setUpProject(t, envs...).f

	for _, status := range statuses {
		for _, opt := range []string{"--pipe", "--all"} {
			_, stdErr, err := f.RunCombinedOutput("ssh", "-p", layoutProjectID, "-e", status, opt)
			assertExitCode(t, 1, err)
			assert.Contains(t, stdErr, "is "+status+", so an SSH connection is not possible", opt)
		}
	}
}
