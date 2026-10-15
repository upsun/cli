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

func TestDomainList(t *testing.T) {
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
			"domains=/projects/"+projectID+"/domains",
		),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	created, _ := time.Parse(time.RFC3339, "2024-01-15T10:00:00Z")
	apiHandler.SetProjectDomains(projectID, []*mockapi.Domain{
		{
			ID:        "example.com",
			Name:      "example.com",
			Type:      "production",
			IsDefault: true,
			CreatedAt: created,
			UpdatedAt: created,
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID+"/domains/example.com",
				"#edit=/projects/"+projectID+"/domains/example.com",
			),
		},
		{
			ID:        "www.example.com",
			Name:      "www.example.com",
			Type:      "production",
			IsDefault: false,
			CreatedAt: created,
			UpdatedAt: created,
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID+"/domains/www.example.com",
				"#edit=/projects/"+projectID+"/domains/www.example.com",
			),
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// List domains.
	output := f.Run("domains", "-p", projectID)
	assert.Contains(t, output, "example.com")
	assert.Contains(t, output, "www.example.com")

	// Get a specific domain property.
	assertTrimmed(t, "production", f.Run("domain:get", "-p", projectID, "example.com", "-P", "type"))
}

func TestDomainListEmpty(t *testing.T) {
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
			"domains=/projects/"+projectID+"/domains",
		),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Empty domain list outputs a message to stderr.
	// Note: the CLI currently exits non-zero for this, which is arguably a bug
	// (cf. services/certificates which exit 0 for empty lists). Not asserting
	// the exit code so this test won't break if that gets fixed.
	_, stdErr, _ := f.RunCombinedOutput("domains", "-p", projectID)
	assert.Contains(t, stdErr, "No domains found")
}

// setUpDomains serves a project that supports non-production domains, with
// "main" (production) and "staging" environments, and the given production domains.
func setUpDomains(
	t *testing.T, productionDomains ...string,
) (apiHandler *mockapi.Handler, f *cmdFactory, projectID string) {
	apiHandler = mockapi.NewHandler(t)
	projectID = mockapi.ProjectID()
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
	envs := []*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
		makeEnv(projectID, "staging", "staging", "active", "main"),
	}
	for _, e := range envs {
		e.Links["#domains"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/" + e.ID + "/domains"}
	}
	apiHandler.SetEnvironments(envs)
	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"custom_domains": map[string]any{"enabled": true}})
	})
	domains := make([]*mockapi.Domain, 0, len(productionDomains))
	for _, name := range productionDomains {
		domains = append(domains, &mockapi.Domain{ID: name, Name: name, Type: "production"})
	}
	apiHandler.SetProjectDomains(projectID, domains)

	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)
	f = newCommandFactory(t, apiServer.URL, authServer.URL)

	return apiHandler, f, projectID
}

// domainNames returns the names of a project's or an environment's domains.
func domainNames(apiHandler *mockapi.Handler, projectID, environmentID string) []string {
	domains := apiHandler.Domains(projectID, environmentID)
	names := make([]string, 0, len(domains))
	for _, d := range domains {
		names = append(names, d.Name)
	}
	return names
}

func TestDomainDelete(t *testing.T) {
	t.Parallel()
	apiHandler, f, projectID := setUpDomains(t, "example.com", "www.example.com")
	apiHandler.SetEnvironmentDomains(projectID, "staging", []*mockapi.Domain{
		{ID: "staging.example.com", Name: "staging.example.com", Type: "replacement", ReplacementFor: "example.com"},
		{
			ID: "www.staging.example.com", Name: "www.staging.example.com",
			Type: "replacement", ReplacementFor: "www.example.com",
		},
	})

	_, stderr, err := f.RunCombinedOutput("domain:delete", "-p", projectID, "missing.example.com")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Domain not found: missing.example.com")

	// Decline the confirmation.
	_, stderr, err = f.RunInteractive("n\n", "domain:delete", "-p", projectID, "www.example.com")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "If this domain has non-production domains attached to it, they will also be deleted.")
	assert.Equal(t, []string{"example.com", "www.example.com"}, domainNames(apiHandler, projectID, ""))

	_, stderr, err = f.RunCombinedOutput("domain:delete", "-p", projectID, "-e", "staging", "staging.example.com")
	require.NoError(t, err, stderr)
	assert.NotContains(t, stderr, "non-production domains attached")
	assert.Contains(t, stderr, "The domain staging.example.com has been deleted.")
	assert.Contains(t, stderr, "The activity succeeded")
	assert.Equal(t, []string{"www.staging.example.com"}, domainNames(apiHandler, projectID, "staging"))
	assert.Equal(t, []string{"example.com", "www.example.com"}, domainNames(apiHandler, projectID, ""))

	// Deleting a production domain also deletes the domains attached to it.
	_, stderr, err = f.RunCombinedOutput("domain:delete", "-p", projectID, "www.example.com")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "The domain www.example.com has been deleted.")
	assert.Equal(t, []string{"example.com"}, domainNames(apiHandler, projectID, ""))
	assert.Empty(t, domainNames(apiHandler, projectID, "staging"))
}
