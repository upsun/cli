package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestResourcesGet is a regression test for "Worker not found: Array (in app:
// Array)" raised when running resources:get without --app or --worker. The
// command defines those options as VALUE_IS_ARRAY (filters), so an empty
// default ([]) was being cast to string and treated as a worker name in
// Selector::selectRemoteContainer.
func TestResourcesGet(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	projectID, _ := setUpResourcesProject(apiHandler, "org-id-1", nextDeployment(map[string]any{
		"app": map[string]any{
			"name":              "app",
			"type":              "golang:1.23",
			"container_profile": "BALANCED",
			"resources":         map[string]any{"profile_size": "0.1"},
			"instance_count":    1,
			"disk":              512,
		},
	}, map[string]any{
		"BALANCED": map[string]any{
			"0.1": map[string]any{"cpu": "0.1", "memory": "256", "cpu_type": "guaranteed"},
		},
	}))
	f := serveAPI(t, apiHandler)

	stdout, stderr, err := f.RunCombinedOutput("resources:get", "-p", projectID, "-e", "main")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.NotContains(t, stderr, "Array to string conversion")
	assert.NotContains(t, stderr, "Worker not found")
	assert.Contains(t, stdout, "app")
}
