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
	t.Parallel()
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
		Settings:      map[string]any{},
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "AUTO"},
		},
		Services: map[string]mockapi.Service{},
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
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            layoutProjectID,
		DefaultBranch: "main",
		Settings:      map[string]any{},
		Repository:    mockapi.ProjectRepository{URL: layoutGitURL},
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+layoutProjectID,
			"environments=/projects/"+layoutProjectID+"/environments",
		),
	}})
	main := makeEnv(layoutProjectID, "main", "production", "active", nil)
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps:  map[string]mockapi.App{"app": {Name: "app", Type: "golang:1.23", Size: "AUTO"}},
		Services: map[string]mockapi.Service{},
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
			f.useConfig(configFile)
			f.dir = repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)
			appConfigFile := filepath.Join(f.dir, ".platform.app.yaml")
			require.NoError(t, os.WriteFile(appConfigFile, []byte(c.content), 0o600))

			stdOut, stdErr, err := f.RunCombinedOutput("apps", "--format", "tsv", "--columns", "name,path")
			require.NoError(t, err, "stderr: %s", stdErr)
			assertTrimmed(t, "Name\tPath\napp\t", stdOut)
		})
	}
}

// TestAppListDisks checks the storage and instance disk columns of apps and services, with and without flexible
// resources.
func TestAppListDisks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		flexible      bool
		noProjectInfo bool
		wantApps      string
		wantAppDisk   string
	}{
		{name: "flexible", flexible: true, wantApps: "app\t2048\t8192\n", wantAppDisk: "storage"},
		{name: "fixed", wantApps: "app\t\t2048\n", wantAppDisk: "instance_disk"},
		// Without project_info in the deployment, the project's settings are read.
		{name: "flexible without project_info", flexible: true, noProjectInfo: true,
			wantApps: "app\t2048\t8192\n", wantAppDisk: "storage"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			apiHandler := mockapi.NewHandler(t)
			projectID := mockapi.ProjectID()
			apiHandler.SetProjects([]*mockapi.Project{{
				ID: projectID,
				Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
					"environments=/projects/"+projectID+"/environments"),
				DefaultBranch: "main",
				Settings:      map[string]any{"sizing_api_enabled": c.flexible},
			}})
			projectInfo := map[string]any{"settings": map[string]any{"sizing_api_enabled": c.flexible}}
			if c.noProjectInfo {
				projectInfo = nil
			}
			main := makeEnv(projectID, "main", "production", "active", nil)
			main.SetCurrentDeployment(&mockapi.Deployment{
				WebApps: map[string]mockapi.App{"app": {
					Name: "app", Type: "golang:1.23", Size: "AUTO", Disk: 2048,
					Mounts:    map[string]mockapi.Mount{"/cache": {Source: "instance", SourcePath: "cache"}},
					Resources: map[string]any{"disk": map[string]any{"storage": 2048, "instance": 8192, "temporary": 8192}},
				}},
				Services:    map[string]mockapi.Service{"db": {Type: "mariadb:11.4", Size: "AUTO", Disk: 1024}},
				ProjectInfo: projectInfo,
				Links:       mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
			})
			apiHandler.SetEnvironments([]*mockapi.Environment{main})
			f := serveAPI(t, apiHandler)

			run := func(args ...string) (string, string) {
				args = append(args, "-p", projectID, "-e", "main", "--format", "tsv", "--no-header")
				stdout, stderr, err := f.RunCombinedOutput(args...)
				require.NoError(t, err, stderr)
				return stdout, stderr
			}

			stdout, stderr := run("apps", "-c", "name,storage,instance_disk")
			assert.Equal(t, c.wantApps, stdout)
			assert.NotContains(t, stderr, "DEPRECATED")

			stdout, stderr = run("apps", "-c", "name,disk")
			assert.Equal(t, "app\t2048\n", stdout)
			assert.Contains(t, stderr, "The column disk has been replaced by "+c.wantAppDisk+".")

			stdout, _ = run("services", "-c", "name,storage")
			assert.Equal(t, "db\t1024\n", stdout)

			stdout, stderr = run("services", "-c", "name,disk")
			assert.Equal(t, "db\t1024\n", stdout)
			assert.Contains(t, stderr, "The column disk has been replaced by storage.")
		})
	}
}
