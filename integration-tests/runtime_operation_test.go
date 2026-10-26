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

// setUpRuntimeOperations serves a project whose main environment has apps with a "migrate" runtime operation.
func setUpRuntimeOperations(t *testing.T, appNames ...string) (
	f *cmdFactory, apiHandler *mockapi.Handler, projectID, deploymentPath string,
) {
	t.Helper()
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	apiHandler = mockapi.NewHandler(t)

	projectID = mockapi.ProjectID()
	envPath := "/projects/" + projectID + "/environments/main"

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
	})

	deploymentPath = envPath + "/deployments/current"

	// Mock the deployment, in which each app has a "migrate" runtime operation.
	webapps := map[string]any{}
	for _, name := range appNames {
		webapps[name] = map[string]any{
			"name": name,
			"type": "golang:1.23",
			"operations": map[string]any{
				"migrate": map[string]any{
					"role":     "viewer",
					"commands": map[string]any{"start": "php migrate.php", "stop": nil},
				},
			},
		}
	}
	apiHandler.Get(deploymentPath, func(w http.ResponseWriter, r *http.Request) {
		// Build absolute URL so the client treats the #operations link correctly.
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base := scheme + "://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webapps":  webapps,
			"services": map[string]any{},
			"workers":  map[string]any{},
			"routes":   map[string]any{},
			"_links": map[string]any{
				"self":        map[string]any{"href": base + deploymentPath},
				"#operations": map[string]any{"href": base + deploymentPath + "/operations"},
			},
		})
	})

	apiHandler.Post(deploymentPath+"/operations", func(w http.ResponseWriter, _ *http.Request) {
		apiHandler.RespondWithActivity(w, projectID, "main", "environment.operation")
	})

	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	f = newCommandFactory(t, apiServer.URL, authServer.URL)
	return f, apiHandler, projectID, deploymentPath
}

// TestRuntimeOperationRun verifies that `operation:run <name>` passes the
// expected app name (not null) through to execRuntimeOperation, even when the
// --app/--worker options are not provided. PHPStan flagged $appName as
// potentially null at the call site; this test reproduces the exact path.
func TestRuntimeOperationRun(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID, deploymentPath := setUpRuntimeOperations(t, "app")

	t.Run("list", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput("operation:list", "-p", projectID, "-e", "main")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.NotContains(t, stderr, "TypeError")
		assert.NotContains(t, stderr, "must be of type string")
		assert.Contains(t, stdout, "migrate")
		assert.Contains(t, stdout, "app")
	})

	t.Run("run", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput(
			"operation:run", "migrate",
			"-p", projectID, "-e", "main",
			"--no-wait", "--yes",
		)
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.NotContains(t, stderr, "TypeError")
		assert.NotContains(t, stderr, "must be of type string")
		assert.NotContains(t, stderr, "must be of type ?string")
		assert.Contains(t, stderr, "Running operation")
		assert.Contains(t, stderr, "migrate")
		assert.Contains(t, stderr, "app")

		body := jsonObject(t, lastRequest(t, apiHandler, "POST", deploymentPath+"/operations").Body)
		assert.Equal(t, "migrate", body["operation"])
		assert.Equal(t, "app", body["service"], "service (app name) must be a non-null string")
		assert.NotContains(t, body, "parameters")
	})

	t.Run("run_with_parameters", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput(
			"operation:run", "migrate",
			"-p", projectID, "-e", "main",
			"--parameter=--force", "--parameter", "my value,other",
			"--no-wait", "--yes",
		)
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

		body := jsonObject(t, lastRequest(t, apiHandler, "POST", deploymentPath+"/operations").Body)
		assert.Equal(t, []any{"--force", "my value,other"}, body["parameters"])
	})

	for _, result := range []string{mockapi.ActivityResultSuccess, mockapi.ActivityResultFailure} {
		t.Run("run_wait_"+result, func(t *testing.T) {
			apiHandler.SetActivityResult(result)
			stdout, stderr, err := f.RunCombinedOutput("operation:run", "migrate", "-p", projectID, "-e", "main", "--yes")
			if result == mockapi.ActivityResultSuccess {
				require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
				assert.Contains(t, stderr, "The activity succeeded")
			} else {
				require.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
				assert.Contains(t, stderr, "The activity failed")
			}
			assert.Contains(t, stderr, "Mock log for environment.operation")
		})
	}
	apiHandler.SetActivityResult(mockapi.ActivityResultSuccess)

	// Drive the not-found branch: this is the path where $appName starts null
	// and remains null when the operation name doesn't match. The command
	// should exit with an error before reaching execRuntimeOperation.
	t.Run("not_found", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput(
			"operation:run", "does-not-exist",
			"-p", projectID, "-e", "main",
			"--no-wait", "--yes",
		)
		// Expect non-zero exit because operation isn't defined.
		assert.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.NotContains(t, stderr, "TypeError")
		assert.NotContains(t, stderr, "must be of type string")
		assert.Contains(t, stderr, "was not found")
	})
}

// TestRuntimeOperationRunAmbiguous checks that an operation defined on more than one app needs --app or --worker.
func TestRuntimeOperationRunAmbiguous(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID, deploymentPath := setUpRuntimeOperations(t, "app", "other")

	stdout, stderr, err := f.RunCombinedOutput("operation:list", "-p", projectID, "-e", "main", "--format", "csv")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "app,migrate")
	assert.Contains(t, stdout, "other,migrate")

	_, stderr, err = f.RunCombinedOutput("operation:run", "migrate", "-p", projectID, "-e", "main", "--no-wait", "--yes")
	assert.Error(t, err)
	assert.Contains(t, stderr, "More than one operation is defined with the same name. Specify an --app or a --worker.")
	assert.Empty(t, apiHandler.Requests("POST", deploymentPath+"/operations"))

	_, stderr, err = f.RunCombinedOutput(
		"operation:run", "migrate", "--app", "other", "-p", projectID, "-e", "main", "--no-wait", "--yes")
	require.NoError(t, err, stderr)
	body := jsonObject(t, lastRequest(t, apiHandler, "POST", deploymentPath+"/operations").Body)
	assert.Equal(t, "other", body["service"])
}
