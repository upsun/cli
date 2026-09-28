package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setupResourcesSetApp serves a project whose next deployment has a single app
// with the given properties and container profiles. It returns the command
// factory, the project ID and a holder for the deployment PATCH body.
func setupResourcesSetApp(
	t *testing.T, app, containerProfiles map[string]any,
) (f *cmdFactory, projectID string, patchBody *atomic.Value) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

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

	projectID = mockapi.ProjectID()
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
		_ = json.NewEncoder(w).Encode(map[string]any{"sizing_api_enabled": true})
	})
	apiHandler.Get("/organizations/"+orgID+"/profile", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})

	nextPath := "/projects/" + projectID + "/environments/main/deployments/next"
	apiHandler.Get(nextPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webapps":  map[string]any{"app": app},
			"services": map[string]any{},
			"workers":  map[string]any{},
			"routes":   map[string]any{},
			"project_info": map[string]any{
				"settings":     map[string]any{},
				"capabilities": map[string]any{},
			},
			"container_profiles": containerProfiles,
			"_links":             mockapi.MakeHALLinks("self="+nextPath, "#edit="+nextPath),
		})
	})

	patchBody = &atomic.Value{}
	apiHandler.Patch(nextPath, func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(b, &body))
		patchBody.Store(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_embedded": map[string]any{"activities": []any{}},
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	return newCommandFactory(t, apiServer.URL, authServer.URL), projectID, patchBody
}

// patchedApp returns webapps.app from a deployment PATCH body.
func patchedApp(t *testing.T, patchBody *atomic.Value) map[string]any {
	body, ok := patchBody.Load().(map[string]any)
	require.True(t, ok, "deployment PATCH was not received")
	webapps, ok := body["webapps"].(map[string]any)
	require.True(t, ok, "PATCH body missing webapps: %v", body)
	app, ok := webapps["app"].(map[string]any)
	require.True(t, ok, "PATCH body missing webapps.app: %v", body)
	return app
}

// TestResourcesSet_DiskKeywords checks the documented 'default' and 'min'
// values for --disk.
func TestResourcesSet_DiskKeywords(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{"default", 2048},
		{"min", 256},
		{"minimum", 256},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			f, projectID, patchBody := setupResourcesSetApp(t, map[string]any{
				"name":              "app",
				"type":              "golang:1.23",
				"container_profile": "HIGH_CPU",
				"resources": map[string]any{
					"profile_size": "1",
					"minimum":      map[string]any{"disk": 256},
					"default":      map[string]any{"disk": 2048},
				},
				"instance_count": 1,
				"disk":           512,
			}, map[string]any{
				"HIGH_CPU": map[string]any{
					"1": map[string]any{"cpu": "1", "memory": "384", "cpu_type": "shared"},
				},
			})

			stdout, stderr, err := f.RunCombinedOutput(
				"resources:set", "-p", projectID, "-e", "main", "--no-wait", "--yes",
				"--disk", "app:"+c.value,
			)
			require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
			assert.EqualValues(t, c.want, patchedApp(t, patchBody)["disk"])
		})
	}
}

// TestResourcesSet_InteractiveIntegerProfileSizes checks that choosing a
// profile size returns the size, when every size offered is an integer.
func TestResourcesSet_InteractiveIntegerProfileSizes(t *testing.T) {
	f, projectID, patchBody := setupResourcesSetApp(t, map[string]any{
		"name":              "app",
		"type":              "golang:1.23",
		"container_profile": "HIGH_CPU",
		"resources": map[string]any{
			"profile_size": "1",
			// The minimum CPU hides the 0.5 size, leaving only integer sizes.
			"minimum": map[string]any{"cpu": 1, "disk": 512},
			"default": map[string]any{"disk": 512},
		},
		"instance_count": 1,
		"disk":           512,
	}, map[string]any{
		"HIGH_CPU": map[string]any{
			"0.5": map[string]any{"cpu": "0.5", "memory": "224", "cpu_type": "shared"},
			"1":   map[string]any{"cpu": "1", "memory": "384", "cpu_type": "shared"},
			"2":   map[string]any{"cpu": "2", "memory": "768", "cpu_type": "shared"},
		},
	})

	// Choose profile size "2", accept the instance count and disk, confirm.
	stdout, stderr, err := f.RunInteractive(
		"2\n\n\ny\n",
		"resources:set", "-p", projectID, "-e", "main", "--no-wait",
	)
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

	resources, ok := patchedApp(t, patchBody)["resources"].(map[string]any)
	require.True(t, ok, "PATCH body missing webapps.app.resources")
	assert.Equal(t, "2", resources["profile_size"])
}
