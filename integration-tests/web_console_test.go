package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestWebConsole(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	orgID := "org-" + mockapi.NumericID()

	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg(orgID, "cli-tests", "CLI Test Org", "my-user-id", "flexible"),
	})

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
		Organization:  orgID,
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assert.Equal(t, "https://console.cli-tests.example.com\n", f.Run("console", "--browser", "0"))

	assert.Equal(t, "https://console.cli-tests.example.com/cli-tests/"+projectID+"\n",
		f.Run("console", "--browser", "0", "-p", projectID))

	assert.Equal(t, "https://console.cli-tests.example.com/cli-tests/"+projectID+"/main\n",
		f.Run("console", "--browser", "0", "-p", projectID, "-e", "."))

	// The "console" command should be aliased to "web".
	assert.Equal(t, "https://console.cli-tests.example.com\n", f.Run("web", "--browser", "0"))
}

func TestWebConsoleProjectUI(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	projectID := mockapi.ProjectID()
	orgID := "org-" + mockapi.NumericID()
	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg(orgID, "cli-tests", "CLI Test Org", "my-user-id", "flexible"),
	})
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:           projectID,
		Links:        mockapi.MakeHALLinks("self=/projects/" + projectID),
		Organization: orgID,
		ProjectUI:    "https://ui.example.com/projects/" + projectID,
	}})
	f := serveAPI(t, apiHandler)

	// Without service.console_url, the URL is the project's "project_ui" from the Projects API.
	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)
	config := strings.Replace(string(baseConfig), "  console_url: 'https://console.cli-tests.example.com'\n", "", 1)
	require.NotEqual(t, string(baseConfig), config)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(config), 0o600))
	f.useConfig(configFile)

	assert.Equal(t, "https://ui.example.com/projects/"+projectID+"\n", f.Run("console", "--browser", "0", "-p", projectID))
	assert.Len(t, apiHandler.Requests("GET", "/organizations/"+orgID+"/projects/"+projectID), 1)
}
