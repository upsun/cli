package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
					// A multi-line command, to check that the chooser stays on one line.
					"command": "set -e\nphp migrate.php\n",
					"timeout": 300,
				},
			},
			map[string]any{
				"name": "cache-clear",
				"type": "app",
				"run": map[string]any{
					"command": "php cache-clear.php",
					"timeout": 60,
				},
			},
		})
	})

	var ranTask atomic.Value // string
	for _, name := range []string{"migrate", "cache-clear"} {
		apiHandler.Post(envPath+"/tasks/"+name+"/run", func(w http.ResponseWriter, _ *http.Request) {
			ranTask.Store(name)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"_embedded": map[string]any{"activities": []any{}},
			})
		})
	}

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	t.Run("list", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput("task:list", "-p", projectID, "-e", "main", "--format", "plain")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stdout, "migrate")
		assert.Contains(t, stdout, "cache-clear")
	})

	t.Run("run", func(t *testing.T) {
		ranTask.Store("")
		stdout, stderr, err := f.RunCombinedOutput("task:run", "migrate", "-p", projectID, "-e", "main", "--yes")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stderr, "The task has been triggered.")
		assert.Equal(t, "migrate", ranTask.Load())
	})

	// With no task argument, the tasks are offered as a numbered list. They are
	// sorted by name and numbered from 0, so choice 1 is "migrate".
	t.Run("choose", func(t *testing.T) {
		ranTask.Store("")
		stdout, stderr, err := f.RunInteractive("1\n", "task:run", "-p", projectID, "-e", "main")

		combined := stdout + "\n---\n" + stderr
		assert.NotContains(t, combined, "TypeError")
		assert.NotContains(t, combined, "must be of type")
		assert.NotContains(t, combined, "Fatal error")
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

		assert.Contains(t, stderr, "Enter a number to choose a task to run:")
		assert.Contains(t, stderr, "cache-clear (php cache-clear.php)")
		assert.Contains(t, stderr, "migrate (set -e …)")
		assert.Contains(t, stderr, "The task has been triggered.")
		assert.Equal(t, "migrate", ranTask.Load())
	})

	t.Run("no_argument_non_interactive", func(t *testing.T) {
		ranTask.Store("")
		stdout, stderr, err := f.RunCombinedOutput("task:run", "-p", projectID, "-e", "main", "--yes")
		assert.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stderr, "The task argument is required in non-interactive mode.")
		assert.Equal(t, "", ranTask.Load())
	})

	t.Run("not_found", func(t *testing.T) {
		ranTask.Store("")
		stdout, stderr, err := f.RunCombinedOutput("task:run", "does-not-exist", "-p", projectID, "-e", "main", "--yes")
		assert.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stderr, "The task does-not-exist was not found on the environment")
		assert.Contains(t, stderr, "To list tasks, run")
		assert.NotContains(t, stderr, "RequestException")
		assert.NotContains(t, stderr, "resulted in a")
		assert.Equal(t, "", ranTask.Load())
	})
}

// A project with no tasks defined at all should say so, rather than reporting
// the requested task as missing or offering an empty list of choices.
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

	for _, args := range [][]string{
		{"task:run", "migrate", "-p", projectID, "-e", "main", "--yes"},
		{"task:run", "-p", projectID, "-e", "main", "--yes"},
	} {
		stdout, stderr, err := f.RunCombinedOutput(args...)
		assert.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stderr, "No tasks were found on the environment")
	}
}
