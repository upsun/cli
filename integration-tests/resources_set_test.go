package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestResourcesSet_CurrentSizeMissingFromContainerProfiles drives
// resources:set into the path PHPStan level 8 flags in
// ResourcesSetCommand::summarizeChangesPerService: the current
// container-profile size is not present in the deployment's
// container_profiles map, so ResourcesUtil::sizeInfo() returns null,
// and the line 436 call formatCPU(null) violates the declared
// int|float|string parameter type.
//
// Setup: a deployment where the app's current profile_size is "0.5"
// but container_profiles["BALANCED"] only advertises "0.1". A
// --size app:0.1 change is then requested with --dry-run, forcing the
// command to print the previous-vs-new summary before exiting.
func TestResourcesSet_CurrentSizeMissingFromContainerProfiles(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})

	orgID := "org-id-1"
	apiHandler.SetOrgs([]*mockapi.Org{{
		ID:           orgID,
		Type:         "flexible",
		Name:         "acme",
		Label:        "Acme",
		Owner:        myUserID,
		Capabilities: []string{},
		Links: mockapi.MakeHALLinks(
			"self=/organizations/"+url.PathEscape(orgID),
			"profile=/organizations/"+url.PathEscape(orgID)+"/profile",
		),
	}})

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID:           projectID,
		Organization: orgID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
	})

	apiHandler.Get("/projects/"+projectID+"/settings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sizing_api_enabled": true,
		})
	})

	// Organization profile without a resources_limit: skips the
	// trial-limit branch that would otherwise reach into
	// $current['sizes'] (a separate nullable path not under test here).
	apiHandler.Get("/organizations/"+orgID+"/profile", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})

	nextPath := "/projects/" + projectID + "/environments/main/deployments/next"
	apiHandler.Get(nextPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webapps": map[string]any{
				"app": map[string]any{
					"name":              "app",
					"type":              "golang:1.23",
					"container_profile": "BALANCED",
					"resources": map[string]any{
						// Current size "0.5" is intentionally NOT
						// present in container_profiles["BALANCED"]
						// below, so sizeInfo() returns null.
						"profile_size": "0.5",
					},
					"instance_count": 1,
					"disk":           512,
				},
			},
			"services": map[string]any{},
			"workers":  map[string]any{},
			"routes":   map[string]any{},
			"project_info": map[string]any{
				"settings":     map[string]any{},
				"capabilities": map[string]any{},
			},
			"container_profiles": map[string]any{
				"BALANCED": map[string]any{
					"0.1": map[string]any{
						"cpu":      "0.1",
						"memory":   "256",
						"cpu_type": "guaranteed",
					},
				},
			},
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput(
		"resources:set",
		"-p", projectID,
		"-e", "main",
		"--size", "app:0.1",
		"--dry-run",
		"--no-wait",
	)

	// The command should not crash with a PHP TypeError.
	assert.NotContains(t, stderr, "TypeError")
	assert.NotContains(t, stderr, "must be of type")
	assert.NotContains(t, stderr, "Fatal error")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

	// The summary should at least mention the new value.
	assert.Contains(t, stderr+stdout, "CPU")
}
