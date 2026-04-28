package tests

import (
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// initGitRepo creates an empty Git repository in a temporary directory and
// returns its path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, out)
	}
	return dir
}

func gitConfig(t *testing.T, dir, key string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "config", "--get", key).Output()
	if err != nil {
		var ee *exec.ExitError
		if ok := assert.ErrorAs(t, err, &ee); ok && ee.ExitCode() == 1 {
			return ""
		}
		require.NoError(t, err)
	}
	return string(out)
}

func TestProjectSetRemote(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()
	gitURL := "test-user@git.cli-tests.example.com:" + projectID + ".git"
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            projectID,
		Title:         "Set Remote Test",
		Region:        "test-region",
		DefaultBranch: "main",
		Repository:    mockapi.ProjectRepository{URL: gitURL},
		Links:         mockapi.MakeHALLinks("self=/projects/" + url.PathEscape(projectID)),
	}})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	t.Run("with project ID in fresh git repo", func(t *testing.T) {
		// Regression test: the customer-reported bug was that running
		// "set-remote PROJECT_ID" in a fresh Git checkout (no .platform/local
		// config) failed with RootNotFoundException, because the unified
		// Selector ignored the positional argument and tried to detect a
		// current project from disk.
		repo := initGitRepo(t)
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.dir = repo

		_, stdErr, err := f.RunCombinedOutput("set-remote", projectID)
		require.NoError(t, err, "stderr: %s", stdErr)
		assert.Contains(t, stdErr, "Setting the remote project for this repository to:")
		assert.Contains(t, stdErr, projectID)
		assert.NotContains(t, stdErr, "RootNotFoundException")
		assert.NotContains(t, stdErr, "Could not determine the current project")

		configFile := filepath.Join(repo, ".platform", "local", "project.yaml")
		body, readErr := os.ReadFile(configFile)
		require.NoError(t, readErr)
		assert.Contains(t, string(body), "id: "+projectID)

		assert.Equal(t, gitURL+"\n", gitConfig(t, repo, "remote.platform-test.url"))
	})

	t.Run("with unknown project ID", func(t *testing.T) {
		repo := initGitRepo(t)
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.dir = repo

		_, stdErr, err := f.RunCombinedOutput("set-remote", "nonexistent")
		ee := &exec.ExitError{}
		require.ErrorAs(t, err, &ee)
		assert.Equal(t, 1, ee.ExitCode())
		assert.Contains(t, stdErr, "Project not found")

		_, statErr := os.Stat(filepath.Join(repo, ".platform"))
		assert.True(t, os.IsNotExist(statErr), "no project config should be written on failure")
	})

	t.Run("outside a git repository", func(t *testing.T) {
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.dir = t.TempDir()

		_, stdErr, err := f.RunCombinedOutput("set-remote", projectID)
		ee := &exec.ExitError{}
		require.ErrorAs(t, err, &ee)
		assert.Equal(t, 1, ee.ExitCode())
		assert.Contains(t, stdErr, "No Git repository found")
	})

	t.Run("unset when nothing is mapped", func(t *testing.T) {
		repo := initGitRepo(t)
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.dir = repo

		_, stdErr, err := f.RunCombinedOutput("set-remote", "-")
		require.NoError(t, err, "stderr: %s", stdErr)
		assert.Contains(t, stdErr, "This repository is not mapped to a remote project.")
	})

	t.Run("without project ID in non-interactive mode", func(t *testing.T) {
		repo := initGitRepo(t)
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.dir = repo

		_, stdErr, err := f.RunCombinedOutput("set-remote")
		ee := &exec.ExitError{}
		require.ErrorAs(t, err, &ee)
		assert.NotEqual(t, 0, ee.ExitCode())
		assert.Contains(t, stdErr, "Could not determine the current project")
	})
}
