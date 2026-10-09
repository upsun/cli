package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestResourcesSet_DiskKeywords checks the documented 'default' and 'min'
// values for --disk.
func TestResourcesSet_DiskKeywords(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{"default", 2048},
		{"min", 256},
		{"minimum", 256},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			apiHandler := mockapi.NewHandler(t)
			projectID, _ := setUpResourcesProject(apiHandler, "org-id-1", nextDeployment(map[string]any{
				"app": map[string]any{
					"name":              "app",
					"type":              "golang:1.23",
					"container_profile": "HIGH_CPU",
					"resources": map[string]any{
						"profile_size": "1",
						"minimum":      map[string]any{"disk": 256},
						"default":      map[string]any{"disk": 2048},
					},
					"instance_count": 1,
					"disk":           512,
				},
			}, map[string]any{
				"HIGH_CPU": map[string]any{
					"1": map[string]any{"cpu": "1", "memory": "384", "cpu_type": "shared"},
				},
			}))
			f := serveAPI(t, apiHandler)

			stdout, stderr, err := f.RunCombinedOutput(
				"resources:set", "-p", projectID, "-e", "main", "--no-wait", "--yes",
				"--disk", "app:"+c.value,
			)
			require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
			assert.EqualValues(t, c.want, patchedApp(t, apiHandler, projectID)["disk"])
		})
	}
}

// TestResourcesSet_InteractiveIntegerProfileSizes checks that choosing a
// profile size returns the size, when every size offered is an integer.
func TestResourcesSet_InteractiveIntegerProfileSizes(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	projectID, _ := setUpResourcesProject(apiHandler, "org-id-1", nextDeployment(map[string]any{
		"app": map[string]any{
			"name":              "app",
			"type":              "golang:1.23",
			"container_profile": "HIGH_CPU",
			"resources": map[string]any{
				"profile_size": "1",
				// The minimum CPU hides the 0.5 size, leaving only integer sizes.
				"minimum": map[string]any{"cpu": 1, "disk": 512},
				"default": map[string]any{"disk": 512},
			},
			"instance_count": 1,
			"disk":           512,
		},
	}, map[string]any{
		"HIGH_CPU": map[string]any{
			"0.5": map[string]any{"cpu": "0.5", "memory": "224", "cpu_type": "shared"},
			"1":   map[string]any{"cpu": "1", "memory": "384", "cpu_type": "shared"},
			"2":   map[string]any{"cpu": "2", "memory": "768", "cpu_type": "shared"},
		},
	}))
	f := serveAPI(t, apiHandler)

	// Choose profile size "2", accept the instance count and disk, confirm.
	stdout, stderr, err := f.RunInteractive(
		"2\n\n\ny\n",
		"resources:set", "-p", projectID, "-e", "main", "--no-wait",
	)
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

	resources, ok := patchedApp(t, apiHandler, projectID)["resources"].(map[string]any)
	require.True(t, ok, "PATCH body missing webapps.app.resources")
	assert.Equal(t, "2", resources["profile_size"])
}
