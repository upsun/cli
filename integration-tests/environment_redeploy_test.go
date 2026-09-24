package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentRedeploy(t *testing.T) {
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
	main.Links["#redeploy"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/redeploy"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Redeploy an active environment.
	stdOut, stdErr, err := f.RunCombinedOutput("redeploy", "-p", projectID, "-e", ".", "--no-wait")
	assert.NoError(t, err)
	combined := stdOut + stdErr
	assert.Contains(t, combined, "redeploy")

	// Remove the #redeploy link and verify the error.
	noRedeployEnv := makeEnv(projectID, "main", "production", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{noRedeployEnv})
	f.Run("cc")

	_, stdErr, err = f.RunCombinedOutput("redeploy", "-p", projectID, "-e", ".")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "redeploy")
}
