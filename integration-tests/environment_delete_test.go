package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentDelete(t *testing.T) {
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
	feature := makeEnv(projectID, "feature", "development", "active", "main")
	feature.Links["#deactivate"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/feature/deactivate"}
	oldFeature := makeEnv(projectID, "old-feature", "development", "inactive", "main")
	apiHandler.SetEnvironments([]*mockapi.Environment{main, feature, oldFeature})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Delete an inactive environment (Git branch deletion).
	_, stdErr, err := f.RunCombinedOutput("environment:delete", "-p", projectID, "-e", "old-feature", "--no-wait")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Deleting inactive environment")
	assert.Contains(t, stdErr, "old-feature")

	// Delete an active environment (deactivation).
	f.Run("cc")
	_, stdErr, err = f.RunCombinedOutput("environment:delete", "-p", projectID, "-e", "feature", "--no-wait")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Deleting environment")
	assert.Contains(t, stdErr, "feature")

	// Delete an environment with in-progress activity (dirty status).
	dirtyEnv := makeEnv(projectID, "dirty-env", "development", "dirty", "main")
	apiHandler.SetEnvironments([]*mockapi.Environment{main, dirtyEnv})
	f.Run("cc")

	_, stdErr, err = f.RunCombinedOutput("environment:delete", "-p", projectID, "-e", "dirty-env")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "in-progress activity")
}
