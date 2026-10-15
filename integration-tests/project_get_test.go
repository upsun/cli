package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectGet(t *testing.T) {
	t.Parallel()
	p := setUpGitProject(t,
		makeEnv(layoutProjectID, "main", "production", "active", nil),
		makeEnv(layoutProjectID, "feature", "development", "active", "main"),
	)
	source := t.TempDir()
	initRepoOnBranch(t, source, "main")
	runGit(t, source, "checkout", "--quiet", "-b", "feature")
	runGit(t, source, "commit", "--quiet", "--allow-empty", "-m", "Feature")
	p.seed(t, source, "main", "feature")

	_, stdErr, err := p.f.RunCombinedOutput("project:get", layoutProjectID, "clone")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "The project Git Test ("+layoutProjectID+") was successfully downloaded to: clone")
	clone := filepath.Join(p.f.dir, "clone")
	assert.Equal(t, gitRef(t, source, "main"), gitRef(t, clone, "HEAD"))
	assert.Equal(t, layoutGitURL+"\n", gitConfig(t, clone, "remote.platform-test.url"))
	assert.FileExists(t, filepath.Join(clone, "README.md"))
	projectConfig, err := os.ReadFile(filepath.Join(clone, ".platform", "local", "project.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(projectConfig), "id: "+layoutProjectID)

	// Clone another environment's branch, with a shallow clone.
	_, _, err = p.f.RunCombinedOutput("project:get", layoutProjectID, "shallow", "-e", "feature", "--depth", "1")
	require.NoError(t, err)
	shallow := filepath.Join(p.f.dir, "shallow")
	assert.Equal(t, gitRef(t, source, "feature"), gitRef(t, shallow, "HEAD"))
	assert.Empty(t, gitRef(t, shallow, "HEAD~1"), "the clone should have a depth of 1")

	_, stdErr, err = p.f.RunCombinedOutput("project:get", layoutProjectID, "clone")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The destination path already exists: clone")
}

// TestProjectGetEmpty checks that getting a project with an empty repository initializes a local one.
func TestProjectGetEmpty(t *testing.T) {
	t.Parallel()
	p := setUpGitProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil))

	_, stdErr, err := p.f.RunCombinedOutput("project:get", layoutProjectID, "empty")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Your project has been initialized and connected to Platform.sh Testing!")
	dir := filepath.Join(p.f.dir, "empty")
	assert.Equal(t, layoutGitURL+"\n", gitConfig(t, dir, "remote.platform-test.url"))
	assert.FileExists(t, filepath.Join(dir, ".platform", "local", "project.yaml"))
}
