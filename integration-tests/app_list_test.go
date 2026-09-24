package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAppList(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "AUTO"},
		},
		Services: map[string]mockapi.App{},
		Routes:   map[string]any{},
		Workers: map[string]mockapi.Worker{
			"app--worker1": {
				App:    mockapi.App{Name: "app--worker1", Type: "golang:1.23", Size: "AUTO"},
				Worker: mockapi.WorkerInfo{Commands: mockapi.Commands{Start: "sleep 60"}},
			},
		},
		Links: mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})

	envs := []*mockapi.Environment{
		main,
		makeEnv(projectID, "staging", "staging", "active", "main"),
		makeEnv(projectID, "dev", "development", "active", "staging"),
		makeEnv(projectID, "fix", "development", "inactive", "dev"),
	}

	apiHandler.SetEnvironments(envs)

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assertTrimmed(t, `
Name	Type
app	golang:1.23
`, f.Run("apps", "-p", projectID, "-e", ".", "--refresh", "--format", "tsv"))

	assertTrimmed(t, `
+--------------+-------------+-------------------+
| Name         | Type        | Commands          |
+--------------+-------------+-------------------+
| app--worker1 | golang:1.23 | start: 'sleep 60' |
+--------------+-------------+-------------------+
`, f.Run("workers", "-v", "-p", projectID, "-e", "."))

	_, stdErr, err := f.RunCombinedOutput("services", "-p", projectID, "-e", "main")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "No services found")
}

// TestAppListEmptyLocalConfig checks that "apps" tolerates an empty or
// comment-only local app config file.
func TestAppListEmptyLocalConfig(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            layoutProjectID,
		DefaultBranch: "main",
		Repository:    mockapi.ProjectRepository{URL: layoutGitURL},
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+layoutProjectID,
			"environments=/projects/"+layoutProjectID+"/environments",
		),
	}})
	main := makeEnv(layoutProjectID, "main", "production", "active", nil)
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps:  map[string]mockapi.App{"app": {Name: "app", Type: "golang:1.23", Size: "AUTO"}},
		Services: map[string]mockapi.App{},
		Routes:   map[string]any{},
		Workers:  map[string]mockapi.Worker{},
		Links:    mockapi.MakeHALLinks("self=/projects/" + layoutProjectID + "/environments/main/deployment/current"),
	})
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	// The test config has no app config file, so add one.
	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := strings.Replace(string(baseConfig),
		"\nservice:\n", "\nservice:\n  app_config_file: '.platform.app.yaml'\n", 1)
	require.NoError(t, os.WriteFile(configFile, []byte(config), 0o600))

	cases := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "comment only", content: "# TODO\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			f.extraEnv = []string{"CLI_CONFIG_FILE=" + configFile}
			f.dir = repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)
			appConfigFile := filepath.Join(f.dir, ".platform.app.yaml")
			require.NoError(t, os.WriteFile(appConfigFile, []byte(c.content), 0o600))

			stdOut, stdErr, err := f.RunCombinedOutput("apps", "--format", "tsv", "--columns", "name,path")
			require.NoError(t, err, "stderr: %s", stdErr)
			assertTrimmed(t, "Name\tPath\napp\t", stdOut)
		})
	}
}
