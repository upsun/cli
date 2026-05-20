package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentPause(t *testing.T) {
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
	main.Links["#pause"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/pause"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Pause an active environment.
	stdOut, stdErr, err := f.RunCombinedOutput("environment:pause", "-p", projectID, "-e", ".", "--no-wait")
	assert.NoError(t, err)
	// The CLI outputs confirmation and pause messages on stderr.
	combined := stdOut + stdErr
	assert.Contains(t, combined, "pause")
}

func TestEnvironmentResume(t *testing.T) {
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

	main := makeEnv(projectID, "main", "production", "paused", nil)
	main.Links["#resume"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/resume"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Resume a paused environment.
	stdOut, stdErr, err := f.RunCombinedOutput("environment:resume", "-p", projectID, "-e", ".", "--no-wait")
	assert.NoError(t, err)
	combined := stdOut + stdErr
	assert.Contains(t, combined, "resum")
}
