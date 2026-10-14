package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentActivate(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

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
	inactive := makeEnv(projectID, "staging", "staging", "inactive", "main")
	inactive.Links["#activate"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/staging/activate"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main, inactive})
	// Activation checks whether the project supports guaranteed CPU.
	apiHandler.Get("/projects/"+projectID+"/settings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"enable_guaranteed_resources": true})
	})
	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"guaranteed_resources": map[string]any{"enabled": true}})
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Activate an inactive environment.
	_, stdErr, err := f.RunCombinedOutput("environment:activate", "-p", projectID, "-e", "staging", "--no-wait")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Activating environment")

	// Activating an already-active environment (no #activate link) reports it.
	_, stdErr, err = f.RunCombinedOutput("environment:activate", "-p", projectID, "-e", "main")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "already active")
}
