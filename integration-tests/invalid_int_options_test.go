package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestInvalidIntegerOptions checks that non-integer values are rejected for
// integer options, instead of being cast to 0 or passed on unvalidated.
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

	env := []string{"-p", projectID, "-e", "main"}
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			"log --lines",
			append([]string{"environment:log", "access", "--lines", "abc"}, env...),
			"The --lines value must be a non-negative integer.",
		},
		{
			"xdebug --port",
			append([]string{"environment:xdebug", "--port", "abc"}, env...),
			"The --port value must be a non-negative integer.",
		},
		{
			"project:list --page",
			[]string{"project:list", "--page", "abc"},
			"The --page value must be a non-negative integer.",
		},
		{
			"project:list --count",
			[]string{"project:list", "--count", "abc"},
			"The --count value must be a non-negative integer.",
		},
		{
			"auth:browser-login --max-age",
			[]string{"auth:browser-login", "--max-age", "abc"},
			"The --max-age value must be a non-negative integer.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stdErr, err := f.RunCombinedOutput(c.args...)
			assert.Error(t, err)
			assert.Contains(t, stdErr, c.wantErr)
		})
	}
}
