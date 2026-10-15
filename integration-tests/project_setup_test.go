package tests

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

// testProject is the layout project, whose Git URL the CLI detects, served by a mock API.
type testProject struct {
	api     *mockapi.Handler
	f       *cmdFactory
	authURL string
}

// gitProject is a test project with a Git repository served over SSH by a mock server.
type gitProject struct {
	*testProject
	repo string // The bare repository served at layoutGitURL.
}

// setUpProject configures the layout project with environments.
func setUpProject(t *testing.T, envs ...*mockapi.Environment) *testProject {
	t.Helper()
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            layoutProjectID,
		Title:         "Test Project",
		DefaultBranch: "main",
		Repository:    mockapi.ProjectRepository{URL: layoutGitURL},
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+layoutProjectID,
			"environments=/projects/"+layoutProjectID+"/environments",
		),
		// Activation reads the project settings and capabilities.
		Settings: map[string]any{},
	}})
	apiHandler.Get("/projects/"+layoutProjectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})
	apiHandler.SetEnvironments(envs)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	return &testProject{api: apiHandler, f: newCommandFactory(t, apiServer.URL, authServer.URL), authURL: authServer.URL}
}

// actions returns the operation requests made to an environment.
func (p *testProject) actions(environmentID string) []mockapi.EnvironmentAction {
	return p.api.EnvironmentActions(layoutProjectID, environmentID)
}

// env returns a copy of an environment from the mock API, or nil if it does not exist.
func (p *testProject) env(environmentID string) *mockapi.Environment {
	return p.api.Environment(layoutProjectID, environmentID)
}

// setUpGitProject configures the layout project with environments, and
// serves its repository over SSH from a bare repository.
func setUpGitProject(t *testing.T, envs ...*mockapi.Environment) *gitProject {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the mock SSH server runs commands with bash")
	}
	p := &gitProject{testProject: setUpProject(t, envs...)}

	// Git runs e.g. "git-receive-pack 'abcdefghijkl.git'", relative to the server's working directory.
	reposDir := t.TempDir()
	p.repo = filepath.Join(reposDir, layoutProjectID+".git")
	runGit(t, reposDir, "init", "--quiet", "--bare", "--initial-branch=main", p.repo)
	runGit(t, p.repo, "config", "receive.advertisePushOptions", "true")
	// Override any global hooks path.
	runGit(t, p.repo, "config", "core.hooksPath", filepath.Join(p.repo, "hooks"))

	p.serveSSH(t, mockssh.ExecHandler(reposDir, nil))

	return p
}

// serveSSH starts a mock SSH server, to which the CLI connects for all SSH URLs.
func (p *testProject) serveSSH(t *testing.T, handler mockssh.CommandHandler) *mockssh.Server {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the mock SSH server runs commands with bash")
	}
	sshServer, err := mockssh.NewServer(t, p.authURL+"/ssh/authority")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sshServer.Stop() })
	sshServer.CommandHandler = handler
	p.f.extraEnv = append(p.f.extraEnv,
		EnvPrefix+"SSH_OPTIONS=HostName 127.0.0.1\nPort "+strconv.Itoa(sshServer.Port()),
		EnvPrefix+"SSH_HOST_KEYS="+sshServer.HostKeyConfig(),
	)
	return sshServer
}

// remoteEnv returns the environment for commands run by the mock SSH server, with
// fake executables (shell scripts, by name) ahead of the system PATH, and relationships.
func remoteEnv(t *testing.T, scripts map[string]string, relationships map[string]any) []string {
	t.Helper()
	binDir := t.TempDir()
	for name, script := range scripts {
		require.NoError(t, os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/bash\n"+script), 0o755))
	}
	env := append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if relationships != nil {
		data, err := json.Marshal(relationships)
		require.NoError(t, err)
		env = append(env, "PLATFORM_RELATIONSHIPS="+base64.StdEncoding.EncodeToString(data))
	}
	return env
}

// sshEnv returns an active "main" environment with a deployment, and an SSH URL for each app and worker.
func sshEnv(d *mockapi.Deployment) *mockapi.Environment {
	env := makeEnv(layoutProjectID, "main", "production", "active", nil)
	if d.Services == nil {
		d.Services = map[string]mockapi.App{}
	}
	if d.Workers == nil {
		d.Workers = map[string]mockapi.Worker{}
	}
	if d.Routes == nil {
		d.Routes = map[string]any{}
	}
	d.Links = mockapi.MakeHALLinks("self=/projects/" + layoutProjectID + "/environments/main/deployment/current")
	env.SetCurrentDeployment(d)
	for name := range d.WebApps {
		env.Links["pf:ssh:"+name] = mockapi.HALLink{HREF: "ssh://" + layoutProjectID + "-main--" + name + "@ssh.cli-tests.example.com"}
	}
	for name := range d.Workers {
		env.Links["pf:ssh:"+name] = mockapi.HALLink{HREF: "ssh://" + layoutProjectID + "-main--" + name + "@ssh.cli-tests.example.com"}
	}
	return env
}

// setHook writes a hook script in the served repository.
func (p *gitProject) setHook(t *testing.T, name, script string) {
	t.Helper()
	path := filepath.Join(p.repo, "hooks", name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755))
}

// seed pushes branches from a local repository to the served repository, bypassing SSH.
func (p *gitProject) seed(t *testing.T, dir string, branches ...string) {
	t.Helper()
	runGit(t, dir, append([]string{"push", "--quiet", p.repo}, branches...)...)
}

// gitRef returns the commit of a ref in a repository, or "" if it does not exist.
func gitRef(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", ref).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
