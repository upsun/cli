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

func TestSourceOperation(t *testing.T) {
	t.Parallel()
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
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#source-operations"] = mockapi.HALLink{HREF: envPath + "/source-operations"}
	main.Links["#source-operation"] = mockapi.HALLink{HREF: envPath + "/source-operation"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get(envPath+"/source-operations", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"operation": "update", "app": "app", "command": "composer update\ngit add composer.lock\ngit commit -m update"},
			{"operation": "reset", "app": "worker", "command": "git reset --hard"},
		})
	})
	apiHandler.Post(envPath+"/source-operation", func(w http.ResponseWriter, _ *http.Request) {
		apiHandler.RespondWithActivity(w, projectID, "main", "environment.source-operation")
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	t.Run("list", func(t *testing.T) {
		stdout, stderr, err := f.RunCombinedOutput("source-operation:list", "-p", projectID, "-e", "main",
			"--format", "csv", "--full")
		require.NoError(t, err, stderr)
		assertTrimmed(t, `
Operation,App,Command
update,app,"composer update
git add composer.lock
git commit -m update"
reset,worker,git reset --hard
`, stdout)
	})

	t.Run("run", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("source-operation:run", "update", "-p", projectID, "-e", "main",
			"--variable", "env:FOO=bar", "--variable", "env:BAZ=qux")
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "Running source operation update")
		assert.Contains(t, stderr, "The activity succeeded")

		assert.JSONEq(t, `{"operation": "update", "variables": {"env": {"FOO": "bar", "BAZ": "qux"}}}`,
			lastRequest(t, apiHandler, "POST", envPath+"/source-operation").Body)
	})

	t.Run("choose", func(t *testing.T) {
		// Operations are sorted by name: "reset" is the first.
		_, stderr, err := f.RunInteractive("0\n", "source-operation:run", "-p", projectID, "-e", "main", "--no-wait")
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "Running source operation reset")

		assert.JSONEq(t, `{"operation": "reset", "variables": {}}`,
			lastRequest(t, apiHandler, "POST", envPath+"/source-operation").Body)
	})

	t.Run("errors", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("source-operation:run", "-p", projectID, "-e", "main")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "The operation argument is required in non-interactive mode.")

		_, stderr, err = f.RunCombinedOutput("source-operation:run", "missing", "-p", projectID, "-e", "main")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "The source operation missing was not found on the environment main")

		_, stderr, err = f.RunCombinedOutput("source-operation:run", "update", "-p", projectID, "-e", "main",
			"--variable", "FOO=bar")
		assert.Error(t, err)
		assert.Contains(t, stderr, "Variables must be defined as type:name=value.")
	})
}
