package tests

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestCertificateList(t *testing.T) {
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

	created, _ := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	expires, _ := time.Parse(time.RFC3339, "2027-01-01T00:00:00Z")
	apiHandler.SetProjectCertificates(projectID, []*mockapi.Certificate{
		{
			ID:            "cert1",
			Domains:       []string{"example.com", "www.example.com"},
			Issuer:        "Custom CA",
			IsProvisioned: false,
			ExpiresAt:     expires,
			CreatedAt:     created,
			UpdatedAt:     created,
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID+"/certificates/cert1",
			),
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// List certificates.
	output := f.Run("certificates", "-p", projectID)
	assert.Contains(t, output, "cert1")
	assert.Contains(t, output, "example.com")

	// Get a specific certificate property.
	assertTrimmed(t, "Custom CA", f.Run("certificate:get", "-p", projectID, "cert1", "-P", "issuer"))
}

func TestCertificateListEmpty(t *testing.T) {
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

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Empty certificate list.
	_, stdErr, err := f.RunCombinedOutput("certificates", "-p", projectID)
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "No certificates found")
}
