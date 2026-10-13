package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestResourcesSet_CurrentSizeMissingFromContainerProfiles drives
// resources:set into the path PHPStan level 8 flags in
// ResourcesSetCommand::summarizeChangesPerService: the current
// container-profile size is not present in the deployment's
// container_profiles map, so ResourcesUtil::sizeInfo() returns null,
// and the line 436 call formatCPU(null) violates the declared
// int|float|string parameter type.
//
// Setup: a deployment where the app's current profile_size is "0.5"
// but container_profiles["BALANCED"] only advertises "0.1". A
// --size app:0.1 change is then requested with --dry-run, forcing the
// command to print the previous-vs-new summary before exiting.
func TestResourcesSet_CurrentSizeMissingFromContainerProfiles(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	// No trial endpoint is mocked: the trial-limit branch that would
	// otherwise reach into $current['sizes'] is skipped (a separate
	// nullable path not under test here).
	projectID, _ := setUpResourcesProject(apiHandler, "org-id-1", nextDeployment(map[string]any{
		"app": map[string]any{
			"name":              "app",
			"type":              "golang:1.23",
			"container_profile": "BALANCED",
			"resources": map[string]any{
				// Current size "0.5" is intentionally NOT present in
				// container_profiles["BALANCED"] below, so sizeInfo()
				// returns null.
				"profile_size": "0.5",
			},
			"instance_count": 1,
			"disk":           512,
		},
	}, map[string]any{
		"BALANCED": map[string]any{
			"0.1": map[string]any{"cpu": "0.1", "memory": "256", "cpu_type": "guaranteed"},
		},
	}))
	f := serveAPI(t, apiHandler)

	stdout, stderr, err := f.RunCombinedOutput(
		"resources:set",
		"-p", projectID,
		"-e", "main",
		"--size", "app:0.1",
		"--dry-run",
		"--no-wait",
	)

	// The command should not crash with a PHP TypeError.
	assert.NotContains(t, stderr, "TypeError")
	assert.NotContains(t, stderr, "must be of type")
	assert.NotContains(t, stderr, "Fatal error")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

	// The summary should at least mention the new value.
	assert.Contains(t, stderr+stdout, "CPU")
}

// TestResourcesSet_SizeBelowMinimumCPU checks that a fractional minimum CPU
// is shown in the error for a profile size below it.
func TestResourcesSet_SizeBelowMinimumCPU(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	projectID, _ := setUpResourcesProject(apiHandler, "org-min-cpu", nextDeployment(map[string]any{
		"app": map[string]any{
			"name":              "app",
			"type":              "golang:1.23",
			"container_profile": "BALANCED",
			"resources": map[string]any{
				"profile_size": "0.5",
				"minimum":      map[string]any{"cpu": 0.25, "memory": 64},
			},
			"instance_count": 1,
			"disk":           512,
		},
	}, map[string]any{
		"BALANCED": map[string]any{
			"0.1": map[string]any{"cpu": 0.1, "memory": 64, "cpu_type": "shared"},
			"0.5": map[string]any{"cpu": 0.5, "memory": 128, "cpu_type": "shared"},
		},
	}))

	_, stderr, err := runResourcesSet(t, apiHandler, projectID, "app:0.1")

	assert.Error(t, err)
	assert.Contains(t, stderr, "its CPU amount 0.1 is below the minimum for this app, 0.25")
}
