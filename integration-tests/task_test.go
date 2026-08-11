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

func TestTaskRun(t *testing.T) {
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
		makeEnv(projectID, "main", "staging", "active", nil),
	})

	apiHandler.Get(envPath+"/tasks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{
				"name": "migrate",
				"type": "app",
				"run": map[string]any{
					"command": "php migrate.php",
					"timeout": 300,
				},
			},
		})
	})

	var runCalled bool
	apiHandler.Post(envPath+"/tasks/migrate/run", func(w http.ResponseWriter, _ *http.Request) {
		runCalled = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_embedded": map[string]any{"activities": []any{}},
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	t.Run("list", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput("task:list", "-p", projectID, "-e", "main", "--format", "plain")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stdout, "migrate")
		assert.Contains(t, stdout, "php migrate.php")
	})

	t.Run("run", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput("task:run", "migrate", "-p", projectID, "-e", "main", "--yes")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stderr, "The task has been triggered.")
		assert.True(t, runCalled, "the task run endpoint was not called")
	})

	t.Run("not_found", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput("task:run", "does-not-exist", "-p", projectID, "-e", "main", "--yes")
		assert.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stderr, "The task does-not-exist was not found on the environment")
		assert.Contains(t, stderr, "To list tasks, run")
		assert.NotContains(t, stderr, "RequestException")
		assert.NotContains(t, stderr, "resulted in a")
	})
}

// A project with no tasks defined at all should say so, rather than reporting
// the requested task as missing.
func TestTaskRunNoTasks(t *testing.T) {
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
		makeEnv(projectID, "main", "staging", "active", nil),
	})

	apiHandler.Get(envPath+"/tasks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{})
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput("task:run", "migrate", "-p", projectID, "-e", "main", "--yes")
	assert.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stderr, "No tasks were found on the environment")
}
