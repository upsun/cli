package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestVariableCreate(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
	}})
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#variables"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/variables"}
	main.Links["#manage-variables"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/variables"}
	envs := []*mockapi.Environment{main}
	apiHandler.SetEnvironments(envs)

	apiHandler.SetProjectVariables(projectID, []*mockapi.Variable{
		{
			Name:         "existing",
			IsSensitive:  true,
			VisibleBuild: true,
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	//nolint:lll
	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", projectID, "-l", "e", "-e", "main", "env:TEST", "--value", "env-level-value")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:TEST on the environment main")

	assertTrimmed(t, "env-level-value", f.Run("var:get", "-p", projectID, "-e", "main", "env:TEST", "-P", "value"))

	//nolint:lll
	_, stdErr, err = f.RunCombinedOutput("var:create", "-p", projectID, "env:TEST", "-l", "p", "--value", "project-level-value")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:TEST on the project "+projectID)

	//nolint:lll
	assertTrimmed(t, "project-level-value", f.Run("var:get", "-p", projectID, "-e", "main", "env:TEST", "-P", "value", "-l", "p"))
	//nolint:lll
	assertTrimmed(t, "env-level-value", f.Run("var:get", "-p", projectID, "-e", "main", "env:TEST", "-P", "value", "-l", "e"))

	_, stdErr, err = f.RunCombinedOutput("var:create", "-p", projectID, "existing", "-l", "p", "--value", "test")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The variable already exists")

	//nolint:lll
	_, _, err = f.RunCombinedOutput("var:update", "-p", projectID, "env:TEST", "-l", "p", "--value", "project-level-value2")
	assert.NoError(t, err)
	assertTrimmed(t, "project-level-value2", f.Run("var:get", "-p", projectID, "env:TEST", "-l", "p", "-P", "value"))

	assertTrimmed(t, "true", f.Run("var:get", "-p", projectID, "env:TEST", "-l", "p", "-P", "visible_runtime"))
	_, _, err = f.RunCombinedOutput("var:update", "-p", projectID, "env:TEST", "-l", "p", "--visible-runtime", "false")
	assert.NoError(t, err)
	assertTrimmed(t, "false", f.Run("var:get", "-p", projectID, "env:TEST", "-l", "p", "-P", "visible_runtime"))
}

func TestVariableCreateWithAppScope(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

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
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app1": {Name: "app1", Type: "golang:1.23"},
			"app2": {Name: "app2", Type: "php:8.3"},
		},
		Routes: map[string]any{},
		Links:  mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", projectID, "-l", "p",
		"env:SCOPED", "--value", "val", "--app-scope", "app1")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:SCOPED")

	out := f.Run("var:get", "-p", projectID, "-l", "p", "env:SCOPED", "-P", "application_scope")
	assert.Contains(t, out, "app1")

	_, _, err = f.RunCombinedOutput("var:create", "-p", projectID, "-l", "p",
		"env:MULTI", "--value", "val", "--app-scope", "app1", "--app-scope", "app2")
	assert.NoError(t, err)

	out = f.Run("var:get", "-p", projectID, "-l", "p", "env:MULTI", "-P", "application_scope")
	assert.Contains(t, out, "app1")
	assert.Contains(t, out, "app2")

	_, stdErr, err = f.RunCombinedOutput("var:create", "-p", projectID, "-l", "p",
		"env:BAD", "--value", "val", "--app-scope", "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "was not found")

	_, _, err = f.RunCombinedOutput("var:update", "-p", projectID, "-l", "p",
		"env:SCOPED", "--app-scope", "app2")
	assert.NoError(t, err)

	out = f.Run("var:get", "-p", projectID, "-l", "p", "env:SCOPED", "-P", "application_scope")
	assert.Contains(t, out, "app2")
}

func TestVariableCreateWithAppScopeNoDeployment(t *testing.T) {
	// Uses an environment without a deployment, so app-scope validation is skipped.
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

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

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", projectID, "-l", "p",
		"env:ANY_APP", "--value", "val", "--app-scope", "anyapp")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:ANY_APP")

	out := f.Run("var:get", "-p", projectID, "-l", "p", "env:ANY_APP", "-P", "application_scope")
	assert.Contains(t, out, "anyapp")
}
