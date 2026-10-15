package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentInit(t *testing.T) {
	t.Parallel()
	main := makeEnv(layoutProjectID, "main", "production", "active", nil)
	main.AddOperations("initialize")
	p := setUpProject(t, main, makeEnv(layoutProjectID, "feature", "development", "active", "main"))

	_, stdErr, err := p.f.RunCombinedOutput("environment:init", "-p", layoutProjectID,
		"https://github.com/example/template.git")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "with repository https://github.com/example/template.git.")

	_, stdErr, err = p.f.RunCombinedOutput("environment:init", "-p", layoutProjectID,
		"https://github.com/example/template.git", "--profile", "Example")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "with profile Example (https://github.com/example/template.git)")

	actions := p.actions("main")
	require.Len(t, actions, 2)
	assert.Equal(t, "environment.initialize", actions[0].Type)
	assert.Equal(t, map[string]any{
		"profile": "template.git", "repository": "https://github.com/example/template.git",
	}, actions[0].Body)
	assert.Equal(t, "Example", actions[1].Body["profile"])

	_, stdErr, err = p.f.RunCombinedOutput("environment:init", "-p", layoutProjectID, "-e", "feature",
		"https://github.com/example/template.git")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Operation not available: The environment feature can't be initialized.")
}
