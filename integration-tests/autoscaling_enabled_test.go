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

// TestAutoscalingSettingsSetEnabled checks that --enabled is compared with the
// given metric's trigger, not the service-level "enabled" (which the API
// computes as "any trigger is enabled").
func TestAutoscalingSettingsSetEnabled(t *testing.T) {
	cpuOnly := map[string]any{
		"app": map[string]any{
			"enabled": true,
			"triggers": map[string]any{
				"cpu": map[string]any{
					"enabled": true,
					"up":      map[string]any{"threshold": 80, "duration": 60},
					"down":    map[string]any{"threshold": 20, "duration": 60},
				},
				"memory": map[string]any{
					"enabled": false,
					"up":      map[string]any{"threshold": 80, "duration": 60},
					"down":    map[string]any{"threshold": 20, "duration": 60},
				},
			},
			"instances":      map[string]any{"min": 1, "max": 3},
			"scale_cooldown": map[string]any{"up": 300, "down": 300},
		},
	}
	cases := []struct {
		name     string
		services map[string]any
		metric   string
		enabled  string
		expected string
	}{
		{"enable a disabled metric", cpuOnly, "memory", "true", "Autoscaling will become: enabled"},
		{"enable an enabled metric", cpuOnly, "cpu", "true", "nothing to update"},
		{"disable an enabled metric", cpuOnly, "cpu", "false", "Autoscaling will become: disabled"},
		{"disable a disabled metric", cpuOnly, "memory", "false", "nothing to update"},
		{"enable a new service", map[string]any{}, "cpu", "true", "Autoscaling will become: enabled"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			apiHandler := mockapi.NewHandler(t)
			projectID := setUpAutoscaling(apiHandler, c.services)

			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()
			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			// Verbose output shows PHP warnings.
			stdout, stderr, err := f.RunCombinedOutput(
				"autoscaling:set", "-v",
				"-p", projectID,
				"-e", "main",
				"--service", "app",
				"--metric", c.metric,
				"--enabled", c.enabled,
				"--dry-run",
			)
			require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
			assert.NotContains(t, stderr, "Warning")
			assert.Contains(t, stderr, c.expected)
		})
	}
}

// setUpAutoscaling configures a project with an "app" and the given services'
// autoscaling settings.
func setUpAutoscaling(apiHandler *mockapi.Handler, services map[string]any) (projectID string) {
	projectID = mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	autoscalingPath := "/projects/" + projectID + "/environments/main/autoscaling"
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#autoscaling"] = mockapi.HALLink{HREF: autoscalingPath}
	main.Links["#manage-autoscaling"] = mockapi.HALLink{HREF: autoscalingPath}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"autoscaling": map[string]any{"enabled": true, "supports_horizontal_scaling_services": false},
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
					"resources":      map[string]any{"profile_size": "0.1"},
				},
			},
			"services": map[string]any{},
			"workers":  map[string]any{},
			"routes":   map[string]any{},
			"_links":   mockapi.MakeHALLinks("self=" + deploymentPath),
		})
	})

	trigger := map[string]any{
		"enabled": false,
		"up":      map[string]any{"threshold": 80, "duration": 60},
		"down":    map[string]any{"threshold": 20, "duration": 600},
	}
	apiHandler.Get(autoscalingPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"defaults": map[string]any{
				"triggers":       map[string]any{"cpu": trigger, "memory": trigger},
				"scale_cooldown": map[string]any{"up": 300, "down": 300},
				"instances":      map[string]any{"min": 1, "max": 10},
			},
			"services": services,
			"_links":   mockapi.MakeHALLinks("self=" + autoscalingPath),
		})
	})

	return projectID
}
