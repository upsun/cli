package tests

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setUpBuildResources serves a project with build resources of 1 CPU and 2048 MB, and maximums of 4 CPU and 8192 MB.
func setUpBuildResources(t *testing.T) (apiHandler *mockapi.Handler, f *cmdFactory, projectID string) {
	apiHandler = mockapi.NewHandler(t)
	projectID = mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
		Settings: map[string]any{
			"sizing_api_enabled": true,
			"build_resources":    map[string]any{"cpu": 1, "memory": 2048},
		},
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})
	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"build_resources": map[string]any{"max_cpu": 4, "max_memory": 8192},
		})
	})
	return apiHandler, serveAPI(t, apiHandler), projectID
}

func TestBuildResourcesGet(t *testing.T) {
	t.Parallel()
	_, f, projectID := setUpBuildResources(t)

	stdout, stderr, err := f.RunCombinedOutput("resources:build:get", "-p", projectID)
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "Build resources for the project")
	assert.Contains(t, stderr, "Configure resources by running: platform-test resources:build:set")
	assertTrimmed(t, `
+-----+-------------+
| CPU | Memory (MB) |
+-----+-------------+
| 1   | 2048        |
+-----+-------------+`, stdout)

	assertTrimmed(t, "CPU\tMemory (MB)\n1\t2048", f.Run("resources:build:get", "-p", projectID, "--format", "tsv"))
}

func TestBuildResourcesSet(t *testing.T) {
	t.Parallel()

	t.Run("options", func(t *testing.T) {
		t.Parallel()
		apiHandler, f, projectID := setUpBuildResources(t)

		stdout, stderr, err := f.RunCombinedOutput("resources:build:set", "-p", projectID, "--cpu", "2", "--memory", "2048")
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "CPU: increasing from 1.0 to 2.0")
		assert.Contains(t, stderr, "Memory: 2048 MB")
		assert.Contains(t, stderr, "The settings were successfully updated.")
		assert.Contains(t, stdout, "| 2   | 2048        |")

		patches := apiHandler.ProjectSettingsPatches(projectID)
		require.Len(t, patches, 1)
		assert.Equal(t, map[string]any{"build_resources": map[string]any{"cpu": 2.0}}, patches[0])
	})

	t.Run("interactive", func(t *testing.T) {
		t.Parallel()
		apiHandler, f, projectID := setUpBuildResources(t)

		// Enter the CPU, accept the current memory, and confirm.
		_, stderr, err := f.RunInteractive("0.5\n\ny\n", "resources:build:set", "-p", projectID)
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "CPU: decreasing from 1.0 to 0.5")

		patches := apiHandler.ProjectSettingsPatches(projectID)
		require.Len(t, patches, 1)
		assert.Equal(t, map[string]any{"build_resources": map[string]any{"cpu": 0.5}}, patches[0])
	})

	t.Run("validation", func(t *testing.T) {
		t.Parallel()
		apiHandler, f, projectID := setUpBuildResources(t)

		cases := []struct {
			args    []string
			wantErr string
		}{
			{[]string{"--cpu", "abc"}, "The CPU value must be a number"},
			{[]string{"--cpu", "0.05"}, "The minimum allowed CPU is 0.1"},
			{[]string{"--cpu", "5"}, "The maximum allowed CPU is 4.0"},
			{[]string{"--memory", "1.5"}, "The memory value must be an integer"},
			{[]string{"--memory", "32"}, "The minimum allowed memory is 64 MB"},
			{[]string{"--memory", "9000"}, "The maximum allowed memory is 8192 MB"},
		}
		for _, c := range cases {
			_, stderr, err := f.RunCombinedOutput(append([]string{"resources:build:set", "-p", projectID}, c.args...)...)
			assertExitCode(t, 1, err)
			assert.Contains(t, stderr, c.wantErr)
		}

		_, stderr, err := f.RunCombinedOutput("resources:build:set", "-p", projectID, "--memory", "2048")
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "No changes were provided: nothing to update.")

		assert.Empty(t, apiHandler.ProjectSettingsPatches(projectID))
	})
}
