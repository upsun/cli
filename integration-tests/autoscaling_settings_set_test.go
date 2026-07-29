package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestAutoscalingSettingsSetMissingDuration verifies behavior of
// AutoscalingSettingsSetCommand when current autoscaling settings are missing
// the "down" trigger sub-array. PHPStan flags this (level 8) because
// summarizeChangesPerService passes the result of a ternary that can yield
// null to formatDurationChange(int|string, int|string).
//
// To hit the path: call autoscaling:set non-interactively with --duration-down
// against a mock whose current settings have triggers.cpu but no
// triggers.cpu.down. summarizeChanges then runs formatDurationChange(null, ...).
func TestAutoscalingSettingsSetMissingDuration(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#autoscaling"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/autoscaling"}
	main.Links["#manage-autoscaling"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/autoscaling"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"autoscaling": map[string]any{
				"enabled":                              true,
				"supports_horizontal_scaling_services": false,
			},
		})
	})

	deploymentPath := "/projects/" + projectID + "/environments/main/deployments/current"
	apiHandler.Get(deploymentPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webapps": map[string]any{
				"app": map[string]any{
					"name":           "app",
					"type":           "golang:1.23",
					"instance_count": 1,
					"disk":           512,
					"resources": map[string]any{
						"profile_size": "0.1",
					},
				},
			},
			"services": map[string]any{},
			"workers":  map[string]any{},
			"routes":   map[string]any{},
			"_links": mockapi.MakeHALLinks(
				"self=" + deploymentPath,
			),
		})
	})

	// Current autoscaling settings — note "triggers.cpu" exists but has no
	// "down" sub-array, which is what triggers the formatDurationChange(null)
	// call in summarizeChangesPerService.
	autoscalingPath := "/projects/" + projectID + "/environments/main/autoscaling"
	apiHandler.Get(autoscalingPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"defaults": map[string]any{
				"triggers": map[string]any{
					"cpu": map[string]any{
						"up":   map[string]any{"threshold": 80, "duration": 60},
						"down": map[string]any{"threshold": 20, "duration": 60},
					},
					"memory": map[string]any{
						"up":   map[string]any{"threshold": 80, "duration": 60},
						"down": map[string]any{"threshold": 20, "duration": 60},
					},
				},
				"scale_cooldown": map[string]any{"up": 300, "down": 300},
				"instances":      map[string]any{"min": 1, "max": 10},
			},
			"services": map[string]any{
				"app": map[string]any{
					"enabled": true,
					"triggers": map[string]any{
						"cpu": map[string]any{
							"enabled": true,
							"up":      map[string]any{"threshold": 80, "duration": 60},
							// No "down" key — this is what PHPStan flags.
						},
					},
					"instances": map[string]any{"min": 1, "max": 3},
					// Also omit scale_cooldown to hit the cooldown variant.
				},
			},
			"_links": mockapi.MakeHALLinks(
				"self=" + autoscalingPath,
			),
		})
	})

	apiHandler.Patch(autoscalingPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// Use --dry-run to skip the confirmation; we just need summarizeChanges to run.
	stdout, stderr, err := f.RunCombinedOutput(
		"autoscaling:set",
		"-p", projectID,
		"-e", "main",
		"--service", "app",
		"--metric", "cpu",
		"--duration-down", "2m",
		"--cooldown-down", "5m",
		"--dry-run",
	)

	combined := stdout + "\n---\n" + stderr
	assert.NotContains(t, combined, "TypeError", "stdout: %s\nstderr: %s", stdout, stderr)
	assert.NotContains(t, combined, "must be of type int|string, null given")
	assert.NotContains(t, combined, "Fatal error")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
}
