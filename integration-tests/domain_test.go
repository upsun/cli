package tests

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestDomainList(t *testing.T) {
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
			SSL:       &mockapi.DomainSSL{HasCertificate: true},
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
			SSL:       &mockapi.DomainSSL{HasCertificate: true},
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

	// Empty domain list — the CLI exits with code 1 for "No domains found".
	_, stdErr, _ := f.RunCombinedOutput("domains", "-p", projectID)
	assert.Contains(t, stdErr, "No domains found")
}
