package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestIntegrationList(t *testing.T) {
	t.Parallel()
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
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	created, _ := time.Parse(time.RFC3339, "2024-06-01T10:00:00Z")
	apiHandler.SetProjectIntegrations(projectID, []*mockapi.Integration{
		{
			ID:         "int1",
			Type:       "github",
			Repository: "org/repo",
			CreatedAt:  created,
			UpdatedAt:  created,
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID+"/integrations/int1",
				"#edit=/projects/"+projectID+"/integrations/int1",
			),
		},
		{
			ID:        "int2",
			Type:      "webhook",
			URL:       "https://hooks.example.com/notify",
			Events:    []string{"environment.push"},
			CreatedAt: created,
			UpdatedAt: created,
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID+"/integrations/int2",
				"#edit=/projects/"+projectID+"/integrations/int2",
			),
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// List integrations.
	output := f.Run("integrations", "-p", projectID)
	assert.Contains(t, output, "int1")
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "int2")
	assert.Contains(t, output, "webhook")

	// Get a specific integration.
	assertTrimmed(t, "github", f.Run("integration:get", "-p", projectID, "int1", "-P", "type"))
	assertTrimmed(t, "org/repo", f.Run("integration:get", "-p", projectID, "int1", "-P", "repository"))
}

func TestIntegrationWrite(t *testing.T) {
	t.Parallel()
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
			"integrations=/projects/"+projectID+"/integrations",
		),
		DefaultBranch: "main",
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})
	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"integrations": map[string]any{
			"enabled": true,
			"config":  map[string]any{"webhook": map[string]any{"enabled": true}},
		}})
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stderr, err := f.RunCombinedOutput("integration:add", "-p", projectID, "--type", "slack", "--channel", "a")
	assert.Error(t, err)
	assert.Contains(t, stderr, "slack is not one of: webhook")

	// Add.
	_, stderr, err = f.RunCombinedOutput("integration:add", "-p", projectID, "--type", "webhook",
		"--url", "https://hooks.example.com/a", "--events", "environment.push,environment.branch")
	require.NoError(t, err, stderr)
	integrations := apiHandler.Integrations(projectID)
	require.Len(t, integrations, 1)
	id := integrations[0].ID
	assert.Contains(t, stderr, "Created integration "+id+" (type: webhook)")
	assert.Equal(t, "https://hooks.example.com/a", integrations[0].URL)
	assert.Equal(t, []string{"environment.push", "environment.branch"}, integrations[0].Events)

	// Update.
	_, stderr, err = f.RunCombinedOutput("integration:update", "-p", projectID, id, "--url", "https://hooks.example.com/a")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "No changed values were provided to update.")

	_, stderr, err = f.RunCombinedOutput("integration:update", "-p", projectID, id, "--url", "https://hooks.example.com/b")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "Integration "+id+" (webhook) updated")
	assert.Equal(t, "https://hooks.example.com/b", apiHandler.Integrations(projectID)[0].URL)

	_, stderr, err = f.RunCombinedOutput("integration:update", "-p", projectID, "--url", "https://hooks.example.com/b")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "An integration ID is required.")

	// Like the API, the mock does not support validating webhooks.
	_, stderr, err = f.RunCombinedOutput("integration:validate", "-p", projectID, id)
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "This integration does not support validation.")

	// Delete, by a partial ID.
	_, stderr, err = f.RunCombinedOutput("integration:delete", "-p", projectID, id[:5])
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "Deleted integration "+id)
	assert.Empty(t, apiHandler.Integrations(projectID))

	_, stderr, err = f.RunCombinedOutput("integration:delete", "-p", projectID, "missing")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, `Integration not found: "missing"`)
}

func TestIntegrationValidateErrors(t *testing.T) {
	t.Parallel()
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
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})

	path := "/projects/" + projectID + "/integrations/"
	apiHandler.SetProjectIntegrations(projectID, []*mockapi.Integration{
		{
			ID:    "invalid",
			Type:  "github",
			Links: mockapi.MakeHALLinks("self="+path+"invalid", "#validate="+path+"invalid/check"),
		},
		{
			ID:    "valid",
			Type:  "gitlab",
			Links: mockapi.MakeHALLinks("self="+path+"valid", "#validate="+path+"valid/validate"),
		},
		{
			ID:    "unsupported",
			Type:  "health.email",
			Links: mockapi.MakeHALLinks("self=" + path + "unsupported"),
		},
	})
	apiHandler.Post(path+"invalid/check", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"detail": map[string]any{"token": "The token is invalid."}})
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stderr, err := f.RunCombinedOutput("integration:validate", "-p", projectID, "valid")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "The integration is valid.")

	stdout, stderr, err := f.RunCombinedOutput("integration:validate", "-p", projectID, "invalid")
	assertExitCode(t, 4, err)
	assert.Contains(t, stderr, "The following error was found:")
	assertTrimmed(t, "token: The token is invalid.", stdout)

	_, stderr, err = f.RunCombinedOutput("integration:validate", "-p", projectID, "unsupported")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "This integration does not support validation.")
}
