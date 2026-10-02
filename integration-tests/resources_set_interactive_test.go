package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestResourcesSet_Interactive exercises the interactive resources:set form:
// the profile size, instance count and disk prompts, change detection, and the
// resulting deployment update. Accepting every default must change nothing;
// entering new values must submit them in the deployment PATCH body.
func TestResourcesSet_Interactive(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	projectID, _ := setUpResourcesProject(apiHandler, "org-id-1", nextDeployment(map[string]any{
		"app": map[string]any{
			"name":              "app",
			"type":              "golang:1.23",
			"container_profile": "HIGH_CPU",
			"resources": map[string]any{
				"profile_size": "0.5",
				"minimum":      map[string]any{"disk": 512},
				"default":      map[string]any{"disk": 512},
			},
			"instance_count": 1,
			"disk":           512,
		},
	}, map[string]any{
		"HIGH_CPU": map[string]any{
			"0.5": map[string]any{"cpu": "0.5", "memory": "224", "cpu_type": "shared"},
			"1":   map[string]any{"cpu": "1", "memory": "384", "cpu_type": "shared"},
		},
	}))
	f := serveAPI(t, apiHandler)

	t.Run("accepting defaults changes nothing", func(t *testing.T) {
		// Newlines accept the profile size, instance count and disk defaults.
		stdout, stderr, err := f.RunInteractive(
			"\n\n\n",
			"resources:set", "-p", projectID, "-e", "main", "--no-wait",
		)

		combined := stdout + "\n---\n" + stderr
		assert.NotContains(t, combined, "TypeError")
		assert.NotContains(t, combined, "must be of type")
		assert.NotContains(t, combined, "Fatal error")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

		assert.Contains(t, stderr, "Enter the number of instances")
		assert.Contains(t, combined, "nothing to update")
		assert.Empty(t, apiHandler.DeploymentPatches(projectID, "main"), "no deployment update should be submitted")
	})

	t.Run("entering new values submits them", func(t *testing.T) {
		// Choose profile size "1", set 2 instances and a 1024 MB disk, confirm.
		stdout, stderr, err := f.RunInteractive(
			"1\n2\n1024\ny\n",
			"resources:set", "-p", projectID, "-e", "main", "--no-wait",
		)

		combined := stdout + "\n---\n" + stderr
		assert.NotContains(t, combined, "TypeError")
		assert.NotContains(t, combined, "must be of type")
		assert.NotContains(t, combined, "Fatal error")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

		assert.Contains(t, stderr, "Summary of changes")
		assert.Contains(t, stderr, "Setting the resources")

		app := patchedApp(t, apiHandler, projectID)
		resources, ok := app["resources"].(map[string]any)
		require.True(t, ok, "PATCH body missing webapps.app.resources: %v", app)

		assert.Equal(t, "1", resources["profile_size"])
		assert.EqualValues(t, 2, app["instance_count"])
		assert.EqualValues(t, 1024, app["disk"])
	})
}
