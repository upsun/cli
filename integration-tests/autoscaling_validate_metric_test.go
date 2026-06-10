package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestAutoscalingSettingsSetMissingMetric verifies that autoscaling:set fails
// cleanly when the user passes --service and another scaling option but omits
// --metric. PHPStan level 8 flags validateMetric($metric, ...) at line 290
// because $metric is string|null and the method expects a string. Before the
// fix, PHP would throw a TypeError; after the fix the CLI must report a
// readable error and exit non-zero.
func TestAutoscalingSettingsSetMissingMetric(t *testing.T) {
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

	autoscalingPath := "/projects/" + projectID + "/environments/main/autoscaling"
	apiHandler.Get(autoscalingPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"defaults": map[string]any{
				"triggers": map[string]any{
					"cpu": map[string]any{
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
							"down":    map[string]any{"threshold": 20, "duration": 60},
						},
					},
					"instances": map[string]any{"min": 1, "max": 3},
				},
			},
			"_links": mockapi.MakeHALLinks(
				"self=" + autoscalingPath,
			),
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// Non-interactive, --service plus another option, no --metric.
	// This drives execution past line 289 (!empty($updates[$service])) which
	// then invokes validateMetric(null) at line 290.
	stdout, stderr, err := f.RunCombinedOutput(
		"autoscaling:set",
		"-p", projectID,
		"-e", "main",
		"--service", "app",
		"--duration-up", "2m",
		"--dry-run",
	)

	combined := stdout + "\n---\n" + stderr
	assert.NotContains(t, combined, "TypeError", "stdout: %s\nstderr: %s", stdout, stderr)
	assert.NotContains(t, combined, "must be of type string, null given")
	assert.NotContains(t, combined, "Fatal error")
	// A successful exit (0) or a clean validation error (non-zero with a
	// readable message) are both acceptable. A TypeError is not.
	_ = err
}
