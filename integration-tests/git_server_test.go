package tests

import (
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

// gitProject is a project with a Git repository served over SSH by a mock server.
type gitProject struct {
	api  *mockapi.Handler
	f    *cmdFactory
	repo string // The bare repository served at layoutGitURL.
}

// setUpGitProject configures the layout project (whose Git URL the CLI detects)
// with environments, and serves its repository over SSH from a bare repository.
func setUpGitProject(t *testing.T, envs ...*mockapi.Environment) *gitProject {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the mock SSH server runs commands with bash")
	}
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            layoutProjectID,
		Title:         "Git Test",
		DefaultBranch: "main",
		Repository:    mockapi.ProjectRepository{URL: layoutGitURL},
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+layoutProjectID,
			"environments=/projects/"+layoutProjectID+"/environments",
		),
	}})
	apiHandler.SetEnvironments(envs)
	f := serveAPIWithAuth(t, apiHandler, authServer.URL)

	// Git runs e.g. "git-receive-pack 'abcdefghijkl.git'", relative to the server's working directory.
	reposDir := t.TempDir()
	repo := filepath.Join(reposDir, layoutProjectID+".git")
	runGit(t, reposDir, "init", "--quiet", "--bare", "--initial-branch=main", repo)
	runGit(t, repo, "config", "receive.advertisePushOptions", "true")
	// Override any global hooks path.
	runGit(t, repo, "config", "core.hooksPath", filepath.Join(repo, "hooks"))

	sshServer, err := mockssh.NewServer(t, authServer.URL+"/ssh/authority")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sshServer.Stop() })
	sshServer.CommandHandler = mockssh.ExecHandler(reposDir, nil)
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"SSH_OPTIONS=HostName 127.0.0.1\nPort "+strconv.Itoa(sshServer.Port()),
		EnvPrefix+"SSH_HOST_KEYS="+sshServer.HostKeyConfig(),
	)

	return &gitProject{api: apiHandler, f: f, repo: repo}
}

// serveAPIWithAuth starts a server for the API handler, and returns a command
// factory that uses it and the auth server.
func serveAPIWithAuth(t *testing.T, apiHandler *mockapi.Handler, authURL string) *cmdFactory {
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)
	return newCommandFactory(t, apiServer.URL, authURL)
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
