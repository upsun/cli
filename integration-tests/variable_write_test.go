package tests

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// variableTestSetup holds common test infrastructure for variable tests.
type variableTestSetup struct {
	authServer *mockapi.AuthServer
	apiServer  *httptest.Server
	apiHandler *mockapi.Handler
	projectID  string
	mainEnv    *mockapi.Environment
	factory    *cmdFactory
}

// setupVariableTest creates the common test infrastructure for variable tests.
func setupVariableTest(t *testing.T) *variableTestSetup {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	mainEnv := makeEnv(projectID, "main", "production", "active", nil)
	mainEnv.Links["#variables"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/variables"}
	mainEnv.Links["#manage-variables"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/variables"}

	return &variableTestSetup{
		authServer: authServer,
		apiServer:  apiServer,
		apiHandler: apiHandler,
		projectID:  projectID,
		mainEnv:    mainEnv,
		factory:    newCommandFactory(t, apiServer.URL, authServer.URL),
	}
}

func TestVariableCreate(t *testing.T) {
	t.Parallel()
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	s.apiHandler.SetProjectVariables(s.projectID, []*mockapi.Variable{
		{
			Name:         "existing",
			IsSensitive:  true,
			VisibleBuild: true,
		},
	})

	f, p := s.factory, s.projectID

	//nolint:lll
	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", p, "-l", "e", "-e", "main", "env:TEST", "--value", "env-level-value")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:TEST on the environment main")
	// Regression: var:create must not print "Variable not found" when the
	// variable does not yet exist.
	assert.NotContains(t, stdErr, "Variable not found")

	assertTrimmed(t, "env-level-value", f.Run("var:get", "-p", p, "-e", "main", "env:TEST", "-P", "value"))

	_, stdErr, err = f.RunCombinedOutput("var:create", "-p", p, "env:TEST", "-l", "p", "--value", "project-level-value")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:TEST on the project "+p)
	assert.NotContains(t, stdErr, "Variable not found")

	assertTrimmed(t, "project-level-value", f.Run("var:get", "-p", p, "-e", "main", "env:TEST", "-P", "value", "-l", "p"))
	assertTrimmed(t, "env-level-value", f.Run("var:get", "-p", p, "-e", "main", "env:TEST", "-P", "value", "-l", "e"))

	_, stdErr, err = f.RunCombinedOutput("var:create", "-p", p, "existing", "-l", "p", "--value", "test")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The variable already exists")

	_, _, err = f.RunCombinedOutput("var:update", "-p", p, "env:TEST", "-l", "p", "--value", "project-level-value2")
	assert.NoError(t, err)
	assertTrimmed(t, "project-level-value2", f.Run("var:get", "-p", p, "env:TEST", "-l", "p", "-P", "value"))

	assertTrimmed(t, "true", f.Run("var:get", "-p", p, "env:TEST", "-l", "p", "-P", "visible_runtime"))
	_, _, err = f.RunCombinedOutput("var:update", "-p", p, "env:TEST", "-l", "p", "--visible-runtime", "false")
	assert.NoError(t, err)
	assertTrimmed(t, "false", f.Run("var:get", "-p", p, "env:TEST", "-l", "p", "-P", "visible_runtime"))
}

func TestVariableCreateDefaultEnvironment(t *testing.T) {
	t.Parallel()
	// Regression test for CLI-164: using "-e ." (the default-environment code)
	// must not fail form validation of the --environment option.
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})

	f, p := s.factory, s.projectID

	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", p, "-e", ".", "-l", "e", "env:FOOBAR", "--value", "bar foo")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Selecting default environment")
	assert.Contains(t, stdErr, "Creating variable env:FOOBAR on the environment main")
	assert.NotContains(t, stdErr, "is not one of")

	assertTrimmed(t, "bar foo", f.Run("var:get", "-p", p, "-e", "main", "env:FOOBAR", "-P", "value"))
}

func TestVariableCreateUpdateWithLevel(t *testing.T) {
	t.Parallel()
	// Regression test for CLI-194: "var:create --update" must forward --level to var:update.
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})

	f, p := s.factory, s.projectID

	f.Run("var:create", "-p", p, "-l", "e", "-e", "main", "env:TEST", "--value", "e0")
	f.Run("var:create", "-p", p, "-l", "p", "env:TEST", "--value", "p0")

	//nolint:lll
	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", p, "-u", "-e", "main", "-l", "e", "env:TEST", "--value", "e1")
	assert.NoError(t, err)
	assert.NotContains(t, stdErr, "found at both")
	assertTrimmed(t, "e1", f.Run("var:get", "-p", p, "-e", "main", "env:TEST", "-l", "e", "-P", "value"))
	assertTrimmed(t, "p0", f.Run("var:get", "-p", p, "-e", "main", "env:TEST", "-l", "p", "-P", "value"))

	_, _, err = f.RunCombinedOutput("var:create", "-p", p, "-u", "-e", "main", "-l", "p", "env:TEST", "--value", "p1")
	assert.NoError(t, err)
	assertTrimmed(t, "p1", f.Run("var:get", "-p", p, "-e", "main", "env:TEST", "-l", "p", "-P", "value"))
	assertTrimmed(t, "e1", f.Run("var:get", "-p", p, "-e", "main", "env:TEST", "-l", "e", "-P", "value"))

	// Project level, without an environment.
	s = setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	f, p = s.factory, s.projectID

	f.Run("var:create", "-p", p, "-l", "p", "env:P", "--value", "p0")
	_, _, err = f.RunCombinedOutput("var:create", "-p", p, "-u", "-l", "p", "env:P", "--value", "p1")
	assert.NoError(t, err)
	assertTrimmed(t, "p1", f.Run("var:get", "-p", p, "env:P", "-l", "p", "-P", "value"))

	// Without --level.
	s = setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	f, p = s.factory, s.projectID

	f.Run("var:create", "-p", p, "-l", "e", "-e", "main", "env:E", "--value", "e0")
	_, _, err = f.RunCombinedOutput("var:create", "-p", p, "-u", "-e", "main", "env:E", "--value", "e1")
	assert.NoError(t, err)
	assertTrimmed(t, "e1", f.Run("var:get", "-p", p, "-e", "main", "env:E", "-l", "e", "-P", "value"))
}

func TestVariableCreateWithAppScope(t *testing.T) {
	t.Parallel()
	s := setupVariableTest(t)

	// Set up deployment with app names for validation.
	s.mainEnv.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app1": {Name: "app1", Type: "golang:1.23"},
			"app2": {Name: "app2", Type: "php:8.3"},
		},
		Workers: map[string]mockapi.Worker{
			"app1--queue": {App: mockapi.App{Name: "app1--queue", Type: "golang:1.23"}},
		},
		Tasks: map[string]mockapi.App{
			"task1": {Name: "task1", Type: "php:8.3"},
		},
		Routes: make(map[string]any),
		Links:  mockapi.MakeHALLinks("self=/projects/" + s.projectID + "/environments/main/deployment/current"),
	})
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})

	f, p := s.factory, s.projectID

	// Test creating project-level variable with single app-scope.
	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", p, "-l", "p",
		"env:SCOPED", "--value", "val", "--app-scope", "app1")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:SCOPED")

	// Verify application_scope was set.
	out := f.Run("var:get", "-p", p, "-l", "p", "env:SCOPED", "-P", "application_scope")
	assert.Contains(t, out, "app1")

	// Test creating variable with multiple app scopes.
	_, _, err = f.RunCombinedOutput("var:create", "-p", p, "-l", "p",
		"env:MULTI", "--value", "val", "--app-scope", "app1", "--app-scope", "app2")
	assert.NoError(t, err)

	out = f.Run("var:get", "-p", p, "-l", "p", "env:MULTI", "-P", "application_scope")
	assert.Contains(t, out, "app1")
	assert.Contains(t, out, "app2")

	// Test creating variable scoped to a task.
	_, _, err = f.RunCombinedOutput("var:create", "-p", p, "-l", "p",
		"env:TASK", "--value", "val", "--app-scope", "task1")
	assert.NoError(t, err)

	out = f.Run("var:get", "-p", p, "-l", "p", "env:TASK", "-P", "application_scope")
	assert.Contains(t, out, "task1")

	// Test creating variable scoped to a worker.
	_, _, err = f.RunCombinedOutput("var:create", "-p", p, "-l", "p",
		"env:WORKER", "--value", "val", "--app-scope", "app1--queue")
	assert.NoError(t, err)

	out = f.Run("var:get", "-p", p, "-l", "p", "env:WORKER", "-P", "application_scope")
	assert.Contains(t, out, "app1--queue")

	// Test validation rejects invalid app names (when deployment exists).
	_, stdErr, err = f.RunCombinedOutput("var:create", "-p", p, "-l", "p",
		"env:BAD", "--value", "val", "--app-scope", "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "was not found")

	// Test updating app-scope.
	_, _, err = f.RunCombinedOutput("var:update", "-p", p, "-l", "p",
		"env:SCOPED", "--app-scope", "app2")
	assert.NoError(t, err)

	out = f.Run("var:get", "-p", p, "-l", "p", "env:SCOPED", "-P", "application_scope")
	assert.Contains(t, out, "app2")
}

func TestVariableCreateWithAppScopeNoDeployment(t *testing.T) {
	t.Parallel()
	// Uses an environment without a deployment, so app-scope validation is skipped.
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})

	f, p := s.factory, s.projectID

	// Without a deployment, any app-scope value should be accepted.
	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", p, "-l", "p",
		"env:ANY_APP", "--value", "val", "--app-scope", "anyapp")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:ANY_APP")

	out := f.Run("var:get", "-p", p, "-l", "p", "env:ANY_APP", "-P", "application_scope")
	assert.Contains(t, out, "anyapp")
}

// TestVariableSensitive checks that sensitive values are sent to the API, but not displayed.
func TestVariableSensitive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		level string
		path  string
		args  []string
	}{
		{"project", "/projects/%s/variables", []string{"-l", "p"}},
		{"environment", "/projects/%s/environments/main/variables", []string{"-l", "e", "-e", "main"}},
	}
	for _, c := range cases {
		t.Run(c.level, func(t *testing.T) {
			t.Parallel()
			s := setupVariableTest(t)
			s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
			f, p := s.factory, s.projectID
			path := fmt.Sprintf(c.path, p)
			args := append([]string{"-p", p}, c.args...)

			stdout, stderr, err := f.RunCombinedOutput(append([]string{"var:create", "env:SECRET",
				"--value", "secret-value-1", "--sensitive", "true"}, args...)...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.NotContains(t, stdout+stderr, "secret-value-1")
			reqs := s.apiHandler.Requests("POST", path)
			require.Len(t, reqs, 1)
			assert.JSONEq(t, `true`, jsonField(t, reqs[0].Body, "is_sensitive"))
			assert.JSONEq(t, `"secret-value-1"`, jsonField(t, reqs[0].Body, "value"))

			stdout, stderr, err = f.RunCombinedOutput(append([]string{"var:update", "env:SECRET",
				"--value", "secret-value-2"}, args...)...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.NotContains(t, stdout+stderr, "secret-value")
			reqs = s.apiHandler.Requests("PATCH", path+"/env:SECRET")
			require.Len(t, reqs, 1)
			assert.JSONEq(t, `"secret-value-2"`, jsonField(t, reqs[0].Body, "value"))

			assertTrimmed(t, "true", f.Run(append([]string{"var:get", "env:SECRET", "-P", "is_sensitive"}, args...)...))

			stdout, stderr, err = f.RunCombinedOutput(append([]string{"var:get", "env:SECRET"}, args...)...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Contains(t, stdout, "env:SECRET")
			assert.NotContains(t, stdout+stderr, "secret-value")

			_, stderr, err = f.RunCombinedOutput(append([]string{"var:get", "env:SECRET", "-P", "value"}, args...)...)
			assertExitCode(t, 1, err)
			assert.Contains(t, stderr, "The variable is sensitive, so its value cannot be read.")

			assertTrimmed(t, "env:SECRET,"+c.level+",",
				f.Run("var:list", "-p", p, "-e", "main", "--format", "csv", "--columns", "name,level,value", "--no-header"))
		})
	}
}

// TestVariableUpdateToSensitive checks that an existing variable can be made sensitive.
func TestVariableUpdateToSensitive(t *testing.T) {
	t.Parallel()
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	f, p := s.factory, s.projectID

	f.Run("var:create", "-p", p, "-l", "p", "env:TOKEN", "--value", "token-value")
	assertTrimmed(t, "token-value", f.Run("var:get", "-p", p, "-l", "p", "env:TOKEN", "-P", "value"))

	f.Run("var:update", "-p", p, "-l", "p", "env:TOKEN", "--sensitive", "true")
	assertTrimmed(t, "true", f.Run("var:get", "-p", p, "-l", "p", "env:TOKEN", "-P", "is_sensitive"))
	_, stderr, err := f.RunCombinedOutput("var:get", "-p", p, "-l", "p", "env:TOKEN", "-P", "value")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "The variable is sensitive")
	assert.NotContains(t, f.Run("var:list", "-p", p, "-e", "main", "--format", "tsv"), "token-value")
}

// TestVariableUpdateJSON checks that the value is validated as JSON only while the variable stays JSON.
func TestVariableUpdateJSON(t *testing.T) {
	t.Parallel()
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	f, p := s.factory, s.projectID
	f.Run("var:create", "-p", p, "-l", "p", "cfg", "--json", "true", "--value", `{"a":1}`)

	_, stdErr, err := f.RunCombinedOutput("var:update", "-p", p, "-l", "p", "cfg", "--value", "plain text")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The value is not valid JSON")

	_, _, err = f.RunCombinedOutput("var:update", "-p", p, "-l", "p", "cfg", "--json", "false", "--value", "plain text")
	assert.NoError(t, err)
	assertTrimmed(t, "false", f.Run("var:get", "-p", p, "-l", "p", "cfg", "-P", "is_json"))
	assertTrimmed(t, "plain text", f.Run("var:get", "-p", p, "-l", "p", "cfg", "-P", "value"))

	_, stdErr, err = f.RunCombinedOutput("var:update", "-p", p, "-l", "p", "cfg", "--json", "true")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The value is not valid JSON")
	assert.Len(t, s.apiHandler.Requests("PATCH", "/projects/*/variables/*"), 1)
}

// jsonField returns the JSON encoding of a field of a JSON object.
func jsonField(t *testing.T, data, field string) string {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(data), &m))
	require.Contains(t, m, field)
	return string(m[field])
}

// TestVariableDisableEnable checks that the deprecated commands act on the environment-level variable.
func TestVariableDisableEnable(t *testing.T) {
	t.Parallel()
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	f, p := s.factory, s.projectID
	f.extraEnv = append(f.extraEnv, "COLUMNS=500")

	f.Run("var:create", "-p", p, "-l", "p", "env:FOO", "--value", "p")
	f.Run("var:create", "-p", p, "-l", "p", "env:PROJECT_ONLY", "--value", "p")
	f.Run("var:create", "-p", p, "-l", "e", "-e", "main", "env:FOO", "--value", "e")

	_, _, err := f.RunCombinedOutput("variable:disable", "-p", p, "-e", "main", "env:FOO")
	assert.NoError(t, err)
	assertTrimmed(t, "false", f.Run("var:get", "-p", p, "-e", "main", "-l", "e", "env:FOO", "-P", "is_enabled"))

	_, _, err = f.RunCombinedOutput("variable:enable", "-p", p, "-e", "main", "env:FOO")
	assert.NoError(t, err)
	assertTrimmed(t, "true", f.Run("var:get", "-p", p, "-e", "main", "-l", "e", "env:FOO", "-P", "is_enabled"))

	_, stdErr, err := f.RunCombinedOutput("variable:disable", "-p", p, "-e", "main", "env:PROJECT_ONLY")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Variable not found")
	assert.Empty(t, s.apiHandler.Requests("PATCH", "/projects/*/variables/*"))
}

// TestVariableCreateEnvLevelWithoutEnvironment checks var:create at the environment level without --environment.
func TestVariableCreateEnvLevelWithoutEnvironment(t *testing.T) {
	t.Parallel()
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	f, p := s.factory, s.projectID
	f.extraEnv = append(f.extraEnv, "COLUMNS=500")

	_, stdErr, err := f.RunCombinedOutput("var:create", "-p", p, "-l", "e", "env:FOO", "--value", "x")
	assert.Error(t, err)
	assert.NotContains(t, stdErr, "No environment selected")
	assert.Contains(t, stdErr, "No environment specified.")

	_, stdErr, err = f.RunInteractive("main\n", "var:create", "-p", p, "-l", "e", "env:FOO", "--value", "x")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Creating variable env:FOO on the environment main")
	assertTrimmed(t, "x", f.Run("var:get", "-p", p, "-e", "main", "-l", "e", "env:FOO", "-P", "value"))

	// The existing variable is found once the environment is chosen.
	_, stdErr, err = f.RunInteractive("main\n", "var:create", "-p", p, "-l", "e", "env:FOO", "--value", "y")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The variable already exists: env:FOO")

	_, stdErr, err = f.RunInteractive("main\n", "var:create", "-p", p, "-l", "e", "-u", "env:FOO", "--value", "y")
	assert.NoError(t, err, stdErr)
	assertTrimmed(t, "y", f.Run("var:get", "-p", p, "-e", "main", "-l", "e", "env:FOO", "-P", "value"))
}
