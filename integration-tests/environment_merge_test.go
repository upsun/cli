package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentMerge(t *testing.T) {
	t.Parallel()
	feature := makeEnv(layoutProjectID, "feature", "development", "active", "main")
	feature.AddOperations("merge")
	p := setUpProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil), feature)

	_, stdErr, err := p.f.RunCombinedOutput("merge", "-p", layoutProjectID, "feature")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Merging feature into main")
	actions := p.actions("feature")
	require.Len(t, actions, 1)
	assert.Equal(t, "environment.merge", actions[0].Type)
	activities := p.api.ProjectActivities(layoutProjectID)
	require.Len(t, activities, 1)
	assert.Equal(t, "complete", activities[0].State, "the command should wait for the activity")

	// Declining the confirmation.
	_, _, err = p.f.RunInteractive("n\n", "merge", "-p", layoutProjectID, "feature")
	assertExitCode(t, 1, err)
	assert.Len(t, p.actions("feature"), 1)

	_, stdErr, err = p.f.RunCombinedOutput("merge", "-p", layoutProjectID, "main")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Operation not available: The environment main can't be merged.")
	assert.Contains(t, stdErr, "The environment does not have a parent.")
}
