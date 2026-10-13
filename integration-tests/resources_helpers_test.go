package tests

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setUpResourcesProject configures a user, an org, a project with the sizing
// API enabled, and its "main" environment with the given next deployment.
func setUpResourcesProject(
	apiHandler *mockapi.Handler, orgID string, next map[string]any,
) (projectID string, main *mockapi.Environment) {
	myUserID := "my-user-id"
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiHandler.SetOrgs([]*mockapi.Org{{
		ID:           orgID,
		Type:         "flexible",
		Name:         "acme",
		Label:        "Acme",
		Owner:        myUserID,
		Capabilities: []string{},
		Links:        mockapi.MakeHALLinks("self=/organizations/" + url.PathEscape(orgID)),
	}})

	projectID = mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:           projectID,
		Organization: orgID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
		Settings:      map[string]any{"sizing_api_enabled": true},
	}})

	main = makeEnv(projectID, "main", "production", "active", nil)
	main.SetNextDeployment(next)
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	return projectID, main
}

// nextDeployment returns a next deployment with the given apps and container
// profiles, and no other containers.
func nextDeployment(webapps, containerProfiles map[string]any) map[string]any {
	return map[string]any{
		"webapps":  webapps,
		"services": map[string]any{},
		"workers":  map[string]any{},
		"routes":   map[string]any{},
		"project_info": map[string]any{
			"settings":     map[string]any{},
			"capabilities": map[string]any{},
		},
		"container_profiles": containerProfiles,
	}
}

// serveAPI starts servers for the API handler and for auth, and returns a
// command factory that uses them.
func serveAPI(t *testing.T, apiHandler *mockapi.Handler) *cmdFactory {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)
	return newCommandFactory(t, apiServer.URL, authServer.URL)
}

// deploymentPatch returns the body of the one update to the "main"
// environment's next deployment.
func deploymentPatch(t *testing.T, apiHandler *mockapi.Handler, projectID string) map[string]any {
	patches := apiHandler.DeploymentPatches(projectID, "main")
	require.Len(t, patches, 1, "expected one deployment update")
	return patches[0]
}

// patchedApp returns webapps.app from the deployment update.
func patchedApp(t *testing.T, apiHandler *mockapi.Handler, projectID string) map[string]any {
	body := deploymentPatch(t, apiHandler, projectID)
	webapps, ok := body["webapps"].(map[string]any)
	require.True(t, ok, "PATCH body missing webapps: %v", body)
	app, ok := webapps["app"].(map[string]any)
	require.True(t, ok, "PATCH body missing webapps.app: %v", body)
	return app
}
