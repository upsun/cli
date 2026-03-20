package tests

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentURL(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

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
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Disk: 2048, Mounts: map[string]mockapi.Mount{}},
		},
		Routes: mockRoutes(),
		Links:  mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// --pipe lists all URLs.
	output := f.Run("environment:url", "-p", projectID, "-e", ".", "--pipe")
	assert.Contains(t, output, "https://main.example.com/")
	assert.Contains(t, output, "http://main.example.com/")

	// --primary returns only the primary route URL (the upstream, not redirect).
	output = f.Run("environment:url", "-p", projectID, "-e", ".", "--primary", "--pipe")
	assert.Contains(t, output, "main.example.com/")
	// Only one URL should be returned.
	assert.Equal(t, 1, len(strings.Split(strings.TrimSpace(output), "\n")))
}

func TestEnvironmentURLLocal(t *testing.T) {
	f := &cmdFactory{t: t}
	routes, err := json.Marshal(mockRoutes())
	require.NoError(t, err)
	f.extraEnv = []string{"PLATFORM_ROUTES=" + base64.StdEncoding.EncodeToString(routes)}

	// --pipe lists all URLs.
	output := f.Run("environment:url", "--pipe")
	assert.Contains(t, output, "https://main.example.com/")
	assert.Contains(t, output, "http://main.example.com/")

	// --primary returns only the primary route URL.
	output = f.Run("environment:url", "--primary", "--pipe")
	assert.Contains(t, output, "main.example.com/")
	assert.Equal(t, 1, len(strings.Split(strings.TrimSpace(output), "\n")))
}
