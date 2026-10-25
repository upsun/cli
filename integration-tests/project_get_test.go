package tests

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
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
	assert.Contains(t, stdErr, "The project Test Project ("+layoutProjectID+") was successfully downloaded to: clone")
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

// TestProjectGetEmptyWithoutDefaultBranch checks that getting a project with an empty repository, and without a default
// branch, initializes a local repository.
func TestProjectGetEmptyWithoutDefaultBranch(t *testing.T) {
	t.Parallel()
	p := setUpGitProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil))
	project, err := json.Marshal(&mockapi.Project{
		ID:         layoutProjectID,
		Title:      "Test Project",
		Repository: mockapi.ProjectRepository{URL: layoutGitURL},
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+layoutProjectID,
			"environments=/projects/"+layoutProjectID+"/environments",
		),
	})
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(project, &data))
	data["default_branch"] = nil
	p.api.Get("/projects/"+layoutProjectID, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(data)
	})

	_, stdErr, err := p.f.RunCombinedOutput("project:get", layoutProjectID, "empty", "-e", "main")
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "Your project has been initialized and connected to Platform.sh Testing!")
	assert.FileExists(t, filepath.Join(p.f.dir, "empty", ".platform", "local", "project.yaml"))
}
