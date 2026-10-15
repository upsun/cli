package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentBranch(t *testing.T) {
	t.Parallel()
	main := makeEnv(layoutProjectID, "main", "production", "active", nil)
	main.AddOperations("branch")
	p := setUpGitProject(t, main, makeEnv(layoutProjectID, "dev", "development", "active", "main"))
	repo := repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)
	p.seed(t, repo, "main")
	// The API creates the Git branch when branching.
	p.seed(t, repo, "main:feature")
	p.f.dir = repo

	_, stdErr, err := p.f.RunCombinedOutput("branch", "feature")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Creating a new environment: feature")
	assert.Contains(t, stdErr, "Settings will be copied and data cloned from the parent environment: main")
	assert.Contains(t, stdErr, "Creating local branch feature")
	assert.Contains(t, stdErr, "Setting the upstream for the local branch to: platform-test/feature")
	assert.Equal(t, "refs/heads/feature\n", gitConfig(t, repo, "branch.feature.merge"))
	assert.Equal(t, gitRef(t, repo, "main"), gitRef(t, repo, "HEAD"))

	actions := p.api.EnvironmentActions(layoutProjectID, "main")
	require.Len(t, actions, 1)
	assert.Equal(t, mockapi.EnvironmentAction{
		Type: "environment.branch",
		Body: map[string]any{"name": "feature", "title": "feature", "clone_parent": true},
	}, actions[0])
	feature := p.api.Environment(layoutProjectID, "feature")
	require.NotNil(t, feature)
	assert.Equal(t, "main", feature.Parent)
	assert.Equal(t, "active", feature.Status)

	// The new environment is now selected, from the checked-out branch.
	assert.Equal(t, "feature\n", p.f.Run("environment:info", "id"))

	// Branch without a local checkout, with options.
	_, stdErr, err = p.f.RunCombinedOutput("branch", "staging", "main",
		"--no-checkout", "--no-clone-parent", "--title", "Staging", "--type", "staging")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Creating a new environment: Staging (staging) (type: staging)")
	assert.Contains(t, stdErr, "Settings will be copied from the parent environment: main")
	assert.NotContains(t, stdErr, "local branch")
	assert.Empty(t, gitRef(t, repo, "refs/heads/staging"))
	actions = p.api.EnvironmentActions(layoutProjectID, "main")
	require.Len(t, actions, 2)
	assert.Equal(t, map[string]any{
		"name": "staging", "title": "Staging", "clone_parent": false, "type": "staging",
	}, actions[1].Body)
	assert.Equal(t, "staging\n", p.f.Run("environment:info", "-e", "staging", "type"))

	_, stdErr, err = p.f.RunCombinedOutput("branch", "staging", "main", "--no-checkout")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "The environment staging already exists.")

	// An environment without the operation cannot be branched.
	_, stdErr, err = p.f.RunCombinedOutput("branch", "other", "dev", "--no-checkout")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Operation not available: The environment dev can't be branched.")
}
