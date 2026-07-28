package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestVariableDelete(t *testing.T) {
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
	main.Links["#variables"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/variables"}
	main.Links["#manage-variables"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/variables"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.SetProjectVariables(projectID, []*mockapi.Variable{
		{
			Name:           "to_delete",
			Value:          "val1",
			VisibleBuild:   true,
			VisibleRuntime: true,
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID+"/variables/to_delete",
				"#edit=/projects/"+projectID+"/variables/to_delete",
				"#delete=/projects/"+projectID+"/variables/to_delete",
			),
		},
	})

	apiHandler.SetEnvLevelVariables(projectID, "main", []*mockapi.EnvLevelVariable{
		{
			Variable: mockapi.Variable{
				Name:           "env:TO_DELETE",
				Value:          "envval",
				VisibleRuntime: true,
				Links: mockapi.MakeHALLinks(
					"self=/projects/"+projectID+"/environments/main/variables/env:TO_DELETE",
					"#edit=/projects/"+projectID+"/environments/main/variables/env:TO_DELETE",
					"#delete=/projects/"+projectID+"/environments/main/variables/env:TO_DELETE",
				),
			},
			IsEnabled:     true,
			IsInheritable: false,
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Delete a project-level variable (use -y to confirm).
	_, stdErr, err := f.RunCombinedOutput("var:delete", "-p", projectID, "-l", "p", "-y", "to_delete")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Deleted variable to_delete")

	// Verify it is gone from list.
	stdOut, stdErr, _ := f.RunCombinedOutput("var", "-p", projectID, "-l", "p")
	assert.NotContains(t, stdOut+stdErr, "to_delete")

	// Delete an env-level variable.
	_, stdErr, err = f.RunCombinedOutput("var:delete", "-p", projectID, "-e", "main", "-l", "e", "-y", "env:TO_DELETE")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Deleted variable env:TO_DELETE")
}
