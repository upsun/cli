package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentBranch(t *testing.T) {
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
	main.Links["#branch"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/branch"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Branch from an active environment.
	_, stdErr, err := f.RunCombinedOutput("environment:branch", "-p", projectID, "-e", "main", "--no-wait", "feature-1")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating a new environment")

	// Branch with a custom title.
	f.Run("cc")
	_, stdErr, err = f.RunCombinedOutput("environment:branch", "-p", projectID, "-e", "main", "--no-wait", "--title", "My Feature", "feature-2")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "My Feature")

	// Branch from an environment without the #branch link (operation unavailable).
	noBranchEnv := makeEnv(projectID, "main", "production", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{noBranchEnv})
	f.Run("cc")

	_, stdErr, err = f.RunCombinedOutput("environment:branch", "-p", projectID, "-e", "main", "feature-3")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "can't be branched")
}
