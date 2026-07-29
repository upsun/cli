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

// TestResourcesGet is a regression test for "Worker not found: Array (in app:
// Array)" raised when running resources:get without --app or --worker. The
// command defines those options as VALUE_IS_ARRAY (filters), so an empty
// default ([]) was being cast to string and treated as a worker name in
// Selector::selectRemoteContainer.
func TestResourcesGet(t *testing.T) {
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

	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
	})

	apiHandler.Get("/projects/"+projectID+"/settings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sizing_api_enabled": true,
		})
	})

	nextPath := "/projects/" + projectID + "/environments/main/deployments/next"
	apiHandler.Get(nextPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webapps": map[string]any{
				"app": map[string]any{
					"name":              "app",
					"type":              "golang:1.23",
					"container_profile": "BALANCED",
					"resources": map[string]any{
						"profile_size": "0.1",
					},
					"instance_count": 1,
					"disk":           512,
				},
			},
			"services": map[string]any{},
			"workers":  map[string]any{},
			"routes":   map[string]any{},
			"container_profiles": map[string]any{
				"BALANCED": map[string]any{
					"0.1": map[string]any{
						"cpu":      "0.1",
						"memory":   "256",
						"cpu_type": "guaranteed",
					},
				},
			},
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput("resources:get", "-p", projectID, "-e", "main")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.NotContains(t, stderr, "Array to string conversion")
	assert.NotContains(t, stderr, "Worker not found")
	assert.Contains(t, stdout, "app")
}
