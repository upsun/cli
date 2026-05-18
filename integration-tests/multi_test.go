package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestMulti(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID1 := mockapi.ProjectID()
	projectID2 := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID:    projectID1,
			Title: "Project One",
			Links: mockapi.MakeHALLinks("self=/projects/" + projectID1),
		},
		{
			ID:    projectID2,
			Title: "Project Two",
			Links: mockapi.MakeHALLinks("self=/projects/" + projectID2),
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdOut, stdErr, err := f.RunCombinedOutput(
		"multi", "-p", projectID1+","+projectID2, "--", "pro:info", "title",
	)
	assert.NoError(t, err)
	assert.Contains(t, stdOut, "Project One")
	assert.Contains(t, stdOut, "Project Two")
	assert.Contains(t, stdErr, "Running command on 2 projects")
	assert.Contains(t, stdErr, "("+projectID1+")")
	assert.Contains(t, stdErr, "("+projectID2+")")

	stdOut, _, err = f.RunCombinedOutput(
		"multi", "-p", projectID1, "-p", projectID2, "--", "pro:info", "title",
	)
	assert.NoError(t, err)
	assert.Contains(t, stdOut, "Project One")
	assert.Contains(t, stdOut, "Project Two")

	stdOut, stdErr, err = f.RunCombinedOutput(
		"multi", "-p", "nonexistent-id", "--", "pro:info", "title",
	)
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Project ID(s) not found")
	assert.Contains(t, stdErr, "nonexistent-id")
	assert.NotContains(t, stdOut, "Project One")
}
