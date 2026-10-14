package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/upsun/cli/pkg/mockapi"
)

// configWithLocalProjectFallbacks returns a config that also detects projects from
// .legacy/local/project.yaml and a "legacy" Git remote, as the Upsun CLI does for Platform.sh.
func configWithLocalProjectFallbacks(t *testing.T) string {
	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)
	var cnf map[string]any
	require.NoError(t, yaml.Unmarshal(baseConfig, &cnf))
	detection, ok := cnf["detection"].(map[string]any)
	require.True(t, ok)
	detection["git_remote_fallbacks"] = []string{"legacy"}
	cnf["local"] = map[string]any{"project_config_fallbacks": []string{".legacy/local/project.yaml"}}

	out, err := yaml.Marshal(cnf)
	require.NoError(t, err)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, out, 0o600))

	return configPath
}

func TestLocalProjectFallbacks(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            layoutProjectID,
		Title:         "Fallback Test",
		DefaultBranch: "main",
		Repository:    mockapi.ProjectRepository{URL: layoutGitURL},
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+layoutProjectID,
			"environments=/projects/"+layoutProjectID+"/environments",
		),
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(layoutProjectID, "main", "production", "active", nil),
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	configPath := configWithLocalProjectFallbacks(t)

	writeFallbackProjectConfig := func(t *testing.T, dir string) {
		configDir := filepath.Join(dir, ".legacy", "local")
		require.NoError(t, os.MkdirAll(configDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "project.yaml"), []byte("id: "+layoutProjectID+"\n"), 0o644))
	}

	t.Run("project from the fallback config file", func(t *testing.T) {
		repo := t.TempDir()
		initRepoOnBranch(t, repo, "main")
		writeFallbackProjectConfig(t, repo)
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.useConfig(configPath)
		f.dir = repo

		stdOut, stdErr, err := f.RunCombinedOutput("env:info", "name")
		require.NoError(t, err, "stderr: %s", stdErr)
		assertTrimmed(t, "main", stdOut)

		_, statErr := os.Stat(filepath.Join(repo, ".platform"))
		assert.True(t, os.IsNotExist(statErr), "reading the fallback must not write the primary config")
	})

	t.Run("project from the fallback Git remote", func(t *testing.T) {
		repo := t.TempDir()
		initRepoOnBranch(t, repo, "main")
		runGit(t, repo, "remote", "add", "legacy", layoutGitURL)
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.useConfig(configPath)
		f.dir = repo

		stdOut, stdErr, err := f.RunCombinedOutput("env:info", "name")
		require.NoError(t, err, "stderr: %s", stdErr)
		assertTrimmed(t, "main", stdOut)
	})

	t.Run("set-remote unset removes the fallbacks", func(t *testing.T) {
		repo := t.TempDir()
		initRepoOnBranch(t, repo, "main")
		writeFallbackProjectConfig(t, repo)
		runGit(t, repo, "remote", "add", "legacy", layoutGitURL)
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.useConfig(configPath)
		f.dir = repo

		_, stdErr, err := f.RunCombinedOutput("set-remote", "-", "--yes")
		require.NoError(t, err, "stderr: %s", stdErr)
		assert.Contains(t, stdErr, "This repository is no longer mapped to a project.")

		_, statErr := os.Stat(filepath.Join(repo, ".legacy", "local", "project.yaml"))
		assert.True(t, os.IsNotExist(statErr))
		assert.Empty(t, gitConfig(t, repo, "remote.legacy.url"))
	})
}
