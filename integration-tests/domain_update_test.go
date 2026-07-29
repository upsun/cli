package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestDomainUpdateProjectScope drives the legacy domain:update command with a
// project-only selection (no -e) to verify the project-scoped branch where
// $environment stays null and $project->getDomain($name) is called with the
// argument name. PHPStan flags getDomain($this->domainName) at level 8 because
// $this->domainName is typed ?string; this test asserts that the path runs
// without a TypeError in practice.
func TestDomainUpdateProjectScope(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
			"domains=/projects/"+projectID+"/domains",
			"#manage-domains=/projects/"+projectID+"/domains",
		),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"custom_domains": map[string]any{"enabled": true},
		})
	})

	// Existing project-level domain. Includes _links with self so it can be
	// fully formed.
	apiHandler.Get("/projects/"+projectID+"/domains/example.com", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "example.com",
			"name":       "example.com",
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-01-01T00:00:00Z",
			"ssl": map[string]any{
				"key":         "",
				"certificate": "",
				"chain":       []any{},
			},
			"_links": map[string]any{
				"self": map[string]string{"href": "/projects/" + projectID + "/domains/example.com"},
			},
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput("domain:update", "-p", projectID, "--no-wait", "example.com")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	// With no --cert/--key flags, the command should fall through to the
	// "nothing to update" branch after fetching the project-level domain.
	assert.Contains(t, stderr, "There is nothing to update.",
		"unexpected output - stdout: %s\nstderr: %s", stdout, stderr)
}

// TestDomainGetProjectScope drives domain:get with project-only selection,
// covering the project-level $project->getDomain($name) branch on line 57 of
// DomainGetCommand. The PHPStan finding at line 56 ($environment->getLink) is
// not reached because $forEnvironment is false when -e is not given.
func TestDomainGetProjectScope(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
			"domains=/projects/"+projectID+"/domains",
		),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get("/projects/"+projectID+"/domains/example.com", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "example.com",
			"name":       "example.com",
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-01-01T00:00:00Z",
			"ssl": map[string]any{
				"key":         "",
				"certificate": "",
				"chain":       []any{},
			},
			"_links": map[string]any{
				"self": map[string]string{"href": "/projects/" + projectID + "/domains/example.com"},
			},
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput("domain:get", "-p", projectID, "example.com")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	// The command renders a simple table with the domain name. Check that it
	// printed the domain name.
	assert.Contains(t, stdout, "example.com",
		"expected domain name in output - stdout: %s\nstderr: %s", stdout, stderr)
}
