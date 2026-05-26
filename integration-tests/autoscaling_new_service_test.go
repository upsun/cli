package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestAutoscalingSettingsSetNewService verifies that autoscaling:set handles
// the case where the targeted service exists in the deployment but does not
// yet appear in the project's autoscaling settings. PHPStan level 8 flags
// $current['triggers'] on a nullable $current at lines 599/601 of
// AutoscalingSettingsSetCommand.php — summarizeChangesPerService receives
// null when $settings[$service] is unset.
func TestAutoscalingSettingsSetNewService(t *testing.T) {
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

	// Autoscaling settings with "app" NOT in services — so $current will be
	// null when summarizeChangesPerService is invoked for it.
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
			"services": map[string]any{},
			"_links": mockapi.MakeHALLinks(
				"self=" + autoscalingPath,
			),
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// Enable autoscaling for a service not yet in autoscaling settings.
	stdout, stderr, err := f.RunCombinedOutput(
		"autoscaling:set",
		"-p", projectID,
		"-e", "main",
		"--service", "app",
		"--metric", "cpu",
		"--enabled", "true",
		"--duration-up", "2m",
		"--dry-run",
	)

	combined := stdout + "\n---\n" + stderr
	assert.NotContains(t, combined, "TypeError", "stdout: %s\nstderr: %s", stdout, stderr)
	assert.NotContains(t, combined, "Cannot access offset")
	assert.NotContains(t, combined, "Fatal error", "stdout: %s\nstderr: %s", stdout, stderr)
	_ = err
}
