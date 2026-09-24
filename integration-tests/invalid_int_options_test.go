package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestInvalidIntegerOptions checks that non-integer values are rejected for
// integer options that are otherwise passed to remote commands.
func TestInvalidIntegerOptions(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	projectID := mockapi.ProjectID()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			"log --lines",
			[]string{"environment:log", "access", "--lines", "abc"},
			"The --lines value must be a non-negative integer.",
		},
		{
			"xdebug --port",
			[]string{"environment:xdebug", "--port", "abc"},
			"The --port value must be a non-negative integer.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stdErr, err := f.RunCombinedOutput(append(c.args, "-p", projectID, "-e", "main")...)
			assert.Error(t, err)
			assert.Contains(t, stdErr, c.wantErr)
		})
	}
}
