package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestRuntimeOperationRun verifies that `operation:run <name>` passes the
// expected app name (not null) through to execRuntimeOperation, even when the
// --app/--worker options are not provided. PHPStan flagged $appName as
// potentially null at the call site; this test reproduces the exact path.
func TestRuntimeOperationRun(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()
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

	deploymentPath := envPath + "/deployments/current"

	// Mock the deployment with one webapp that has a "migrate" runtime operation.
	apiHandler.Get(deploymentPath, func(w http.ResponseWriter, r *http.Request) {
		// Build absolute URL so the client treats the #operations link correctly.
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base := scheme + "://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webapps": map[string]any{
				"app": map[string]any{
					"name": "app",
					"type": "golang:1.23",
					"operations": map[string]any{
						"migrate": map[string]any{
							"role": "app",
							"commands": map[string]any{
								"start": "php migrate.php",
								"stop":  nil,
							},
						},
					},
				},
			},
			"services": map[string]any{},
			"workers":  map[string]any{},
			"routes":   map[string]any{},
			"_links": map[string]any{
				"self":        map[string]any{"href": base + deploymentPath},
				"#operations": map[string]any{"href": base + envPath + "/runtime-operations"},
			},
		})
	})

	var receivedBody atomic.Value // map[string]any
	apiHandler.Post(envPath+"/runtime-operations", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		receivedBody.Store(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_embedded": map[string]any{"activities": []any{}},
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

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

		body, ok := receivedBody.Load().(map[string]any)
		require.True(t, ok, "operations POST was not received")
		assert.Equal(t, "migrate", body["operation"])
		assert.Equal(t, "app", body["service"], "service (app name) must be a non-null string")
	})

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
