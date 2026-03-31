package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentMerge(t *testing.T) {
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
	staging := makeEnv(projectID, "staging", "staging", "active", "main")
	staging.Links["#merge"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/staging/merge"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main, staging})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Merge an environment into its parent.
	_, stdErr, err := f.RunCombinedOutput("environment:merge", "-p", projectID, "-e", "staging", "--no-wait")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Merging")
	assert.Contains(t, stdErr, "staging")
	assert.Contains(t, stdErr, "main")

	// Merge unavailable (no #merge link).
	noMergeStaging := makeEnv(projectID, "staging", "staging", "active", "main")
	apiHandler.SetEnvironments([]*mockapi.Environment{main, noMergeStaging})
	f.Run("cc")

	_, stdErr, err = f.RunCombinedOutput("environment:merge", "-p", projectID, "-e", "staging")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "can't be merged")

	// Merge an environment with no parent.
	orphan := makeEnv(projectID, "orphan", "development", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{main, orphan})
	f.Run("cc")

	_, stdErr, err = f.RunCombinedOutput("environment:merge", "-p", projectID, "-e", "orphan")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "can't be merged")
	assert.Contains(t, stdErr, "does not have a parent")
}
