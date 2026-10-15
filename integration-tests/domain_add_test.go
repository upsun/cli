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
	t.Parallel()
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

func TestDomainAddNonProduction(t *testing.T) {
	t.Parallel()

	t.Run("one production domain", func(t *testing.T) {
		t.Parallel()
		apiHandler, f, projectID := setUpDomains(t, "example.com")

		_, stderr, err := f.RunCombinedOutput("domain:add", "-p", projectID, "-e", "staging", "staging.example.com")
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "It will be attached to the production domain: example.com")
		assert.Contains(t, stderr, "The activity succeeded")

		domains := apiHandler.Domains(projectID, "staging")
		require.Len(t, domains, 1)
		assert.Equal(t, "staging.example.com", domains[0].Name)
		assert.Equal(t, "replacement", domains[0].Type)
		assert.Equal(t, "example.com", domains[0].ReplacementFor)
	})

	t.Run("choose a production domain", func(t *testing.T) {
		t.Parallel()
		apiHandler, f, projectID := setUpDomains(t, "example.com", "example.org")

		_, stderr, err := f.RunInteractive("1\ny\n", "domain:add", "-p", projectID, "-e", "staging", "--no-wait",
			"staging.example.org")
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "Choose a production domain:")
		assert.Contains(t, stderr, "It will be attached to the production domain: example.org")

		domains := apiHandler.Domains(projectID, "staging")
		require.Len(t, domains, 1)
		assert.Equal(t, "example.org", domains[0].ReplacementFor)
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		apiHandler, f, projectID := setUpDomains(t)

		_, stderr, err := f.RunCombinedOutput("domain:add", "-p", projectID, "-e", "staging", "staging.example.com")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "No production domains found.")

		_, stderr, err = f.RunCombinedOutput("domain:add", "-p", projectID, "-e", "staging",
			"--attach", "missing.example.com", "staging.example.com")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "The production domain (--attach) was not found: missing.example.com")

		_, stderr, err = f.RunCombinedOutput("domain:add", "-p", projectID, "-e", "main",
			"--attach", "example.com", "www.example.com")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "The --attach option is only valid for non-production environment domains.")

		assert.Empty(t, apiHandler.Domains(projectID, "staging"))
	})
}
