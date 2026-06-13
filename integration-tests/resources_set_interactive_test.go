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

// TestResourcesSet_Interactive exercises the interactive resources:set form:
// the profile size, instance count and disk prompts, change detection, and the
// resulting deployment update. Accepting every default must change nothing;
// entering new values must submit them in the deployment PATCH body.
func TestResourcesSet_Interactive(t *testing.T) {
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

	// No resources_limit, so the trial-limit branch is skipped.
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
					"container_profile": "HIGH_CPU",
					"resources": map[string]any{
						"profile_size": "0.5",
						"minimum":      map[string]any{"disk": 512},
						"default":      map[string]any{"disk": 512},
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
				"HIGH_CPU": map[string]any{
					"0.5": map[string]any{"cpu": "0.5", "memory": "224", "cpu_type": "shared"},
					"1":   map[string]any{"cpu": "1", "memory": "384", "cpu_type": "shared"},
				},
			},
			"_links": mockapi.MakeHALLinks("self="+nextPath, "#edit="+nextPath),
		})
	})

	var patchBody atomic.Value // map[string]any
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
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	t.Run("accepting defaults changes nothing", func(t *testing.T) {
		// Newlines accept the profile size, instance count and disk defaults.
		stdout, stderr, err := f.RunInteractive(
			"\n\n\n",
			"resources:set", "-p", projectID, "-e", "main", "--no-wait",
		)

		combined := stdout + "\n---\n" + stderr
		assert.NotContains(t, combined, "TypeError")
		assert.NotContains(t, combined, "must be of type")
		assert.NotContains(t, combined, "Fatal error")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

		assert.Contains(t, stderr, "Enter the number of instances")
		assert.Contains(t, combined, "nothing to update")
		assert.Nil(t, patchBody.Load(), "no deployment update should be submitted")
	})

	t.Run("entering new values submits them", func(t *testing.T) {
		// Choose profile size "1", set 2 instances and a 1024 MB disk, confirm.
		stdout, stderr, err := f.RunInteractive(
			"1\n2\n1024\ny\n",
			"resources:set", "-p", projectID, "-e", "main", "--no-wait",
		)

		combined := stdout + "\n---\n" + stderr
		assert.NotContains(t, combined, "TypeError")
		assert.NotContains(t, combined, "must be of type")
		assert.NotContains(t, combined, "Fatal error")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

		assert.Contains(t, stderr, "Summary of changes")
		assert.Contains(t, stderr, "Setting the resources")

		body, ok := patchBody.Load().(map[string]any)
		require.True(t, ok, "deployment PATCH was not received")
		webapps, ok := body["webapps"].(map[string]any)
		require.True(t, ok, "PATCH body missing webapps: %v", body)
		app, ok := webapps["app"].(map[string]any)
		require.True(t, ok, "PATCH body missing webapps.app: %v", body)
		resources, ok := app["resources"].(map[string]any)
		require.True(t, ok, "PATCH body missing webapps.app.resources: %v", body)

		assert.Equal(t, "1", resources["profile_size"])
		assert.EqualValues(t, 2, app["instance_count"])
		assert.EqualValues(t, 1024, app["disk"])
	})
}
