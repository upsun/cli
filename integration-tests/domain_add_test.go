package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestDomainAdd is a regression test for a TypeError raised by the legacy
// domain:add command on a production environment when --attach was not given.
func TestDomainAdd(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
			"#manage-domains=/projects/"+projectID+"/domains",
		),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#domains"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/domains"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"custom_domains": map[string]any{"enabled": true},
		})
	})

	var captured map[string]any
	apiHandler.Post("/projects/"+projectID+"/environments/main/domains", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"name": captured["name"]})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput("domain:add", "-p", projectID, "-e", ".", "--no-wait", "example.com")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stderr, "Adding the domain: example.com")
	assert.Equal(t, "example.com", captured["name"])
	assert.NotContains(t, captured, "replacement_for", "request body must omit replacement_for when --attach is not given")
}
