package tests

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestIntegrationList(t *testing.T) {
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
