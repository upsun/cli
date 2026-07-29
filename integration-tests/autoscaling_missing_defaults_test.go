package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestAutoscalingSettingsSetMissingDefaults verifies that autoscaling:set
// fails gracefully when the autoscaling-settings API response omits the
// "defaults" key (or returns null for it). The pre-fix code unconditionally
// dereferenced $defaults['instances']['max'] and called
// getSupportedMetrics($defaults), which is typed array, so PHP raised a
// TypeError on null.
func TestAutoscalingSettingsSetMissingDefaults(t *testing.T) {
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

	// Autoscaling settings with no "defaults" key — the API payload is
	// otherwise valid. The unfixed CLI assumed $defaults was always present.
	autoscalingPath := "/projects/" + projectID + "/environments/main/autoscaling"
	apiHandler.Get(autoscalingPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": map[string]any{},
			"_links": mockapi.MakeHALLinks(
				"self=" + autoscalingPath,
			),
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput(
		"autoscaling:set",
		"-p", projectID,
		"-e", "main",
		"--service", "app",
		"--metric", "cpu",
		"--enabled", "true",
		"--dry-run",
	)

	combined := stdout + "\n---\n" + stderr
	assert.NotContains(t, combined, "TypeError", "stdout: %s\nstderr: %s", stdout, stderr)
	assert.NotContains(t, combined, "must be of type array, null given")
	assert.NotContains(t, combined, "Cannot access offset")
	assert.NotContains(t, combined, "Fatal error", "stdout: %s\nstderr: %s", stdout, stderr)
	// The CLI should exit non-zero with an actionable message.
	assert.Error(t, err, "expected non-zero exit when defaults are missing")
	assert.Contains(t, stderr, "autoscaling", "stderr: %s", stderr)
}
