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

func TestEnvironmentActivateParent(t *testing.T) {
	t.Parallel()
	staging := makeEnv(layoutProjectID, "staging", "staging", "inactive", "main")
	staging.AddOperations("activate")
	apiHandler, f, _ := setUpProject(t,
		makeEnv(layoutProjectID, "main", "production", "active", nil),
		makeEnv(layoutProjectID, "dev", "development", "active", "main"),
		staging,
	)

	_, stdErr, err := f.RunCombinedOutput("environment:activate", "-p", layoutProjectID, "staging", "--parent", "missing")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Parent environment not found: missing")
	assert.Empty(t, apiHandler.EnvironmentActions(layoutProjectID, "staging"))

	_, stdErr, err = f.RunCombinedOutput("environment:activate", "-p", layoutProjectID, "staging", "--parent", "dev")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Setting parent of environment staging to dev")
	assert.Contains(t, stdErr, "Activating environment staging")
	assert.Equal(t, "dev", apiHandler.Environment(layoutProjectID, "staging").Parent)
	actions := apiHandler.EnvironmentActions(layoutProjectID, "staging")
	if assert.Len(t, actions, 1) {
		assert.Equal(t, "environment.activate", actions[0].Type)
	}
}
