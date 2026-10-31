package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestVariableCreateOptions(t *testing.T) {
	t.Parallel()
	s := setupVariableTest(t)
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	f, p := s.factory, s.projectID

	cases := []struct {
		name     string
		args     []string
		wantPath string
		wantBody map[string]any
		wantErr  string
	}{
		{
			name:     "environment level",
			args:     []string{"-l", "e", "-e", "main", "--name", "E1", "--prefix", "env", "--enabled", "false", "--inheritable", "no", "--visible-build", "true", "--visible-runtime", "false"},
			wantPath: "/projects/*/environments/main/variables",
			wantBody: map[string]any{
				"name": "env:E1", "is_enabled": false, "is_inheritable": false, "visible_build": true, "visible_runtime": false,
			},
		},
		{
			name:     "environment level defaults",
			args:     []string{"-l", "e", "-e", "main", "--name", "E2"},
			wantPath: "/projects/*/environments/main/variables",
			// The API's defaults for visibility are left out.
			wantBody: map[string]any{"name": "E2", "is_enabled": true, "is_inheritable": true, "visible_build": nil, "visible_runtime": nil},
		},
		{
			name:     "project level defaults",
			args:     []string{"-l", "p", "--name", "P1", "--prefix", "env:"},
			wantPath: "/projects/*/variables",
			wantBody: map[string]any{"name": "env:P1", "visible_build": true, "visible_runtime": true, "is_enabled": nil},
		},
		{
			name:     "other prefix",
			args:     []string{"-l", "p", "--name", "P2", "--prefix", "custom"},
			wantPath: "/projects/*/variables",
			wantBody: map[string]any{"name": "custom:P2"},
		},
		{
			name:     "no prefix",
			args:     []string{"-l", "p", "--name", "P3", "--prefix", "none"},
			wantPath: "/projects/*/variables",
			wantBody: map[string]any{"name": "P3"},
		},
		{
			name:    "enabled at the project level",
			args:    []string{"-l", "p", "--name", "P4", "--enabled", "false"},
			wantErr: "The option --enabled can only be used for variables at the environment level, not at the project level.",
		},
		{
			name:    "inheritable at the project level",
			args:    []string{"-l", "p", "--name", "P5", "--inheritable", "true"},
			wantErr: "The option --inheritable can only be used for variables at the environment level, not at the project level.",
		},
		{
			name:    "invalid boolean",
			args:    []string{"-l", "p", "--name", "P6", "--visible-build", "maybe"},
			wantErr: "Invalid value for 'Visible at build time': maybe",
		},
		{
			name:    "invalid name after the prefix",
			args:    []string{"-l", "p", "--name", "1P", "--prefix", "env"},
			wantErr: "The environment variable name is invalid: 1P",
		},
		{
			name:    "name with a space",
			args:    []string{"-l", "p", "--name", "P 7"},
			wantErr: "The variable name must not contain a space.",
		},
		{
			name:    "prefix and a prefixed name",
			args:    []string{"-l", "p", "--name", "env:P9", "--prefix", "env"},
			wantErr: "The option --prefix cannot be used, as the name already has a prefix: env:P9",
		},
		{
			name:    "name argument and option",
			args:    []string{"-l", "p", "P8", "--name", "P8"},
			wantErr: "You cannot use both the name argument and --name option.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			countPosts := func() int {
				return len(s.apiHandler.Requests("POST", "/projects/*/variables")) +
					len(s.apiHandler.Requests("POST", "/projects/*/environments/main/variables"))
			}
			before := countPosts()
			args := append([]string{"variable:create", "-p", p, "--value", "v", "--no-wait"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			if c.wantErr != "" {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, c.wantErr)
				assert.Equal(t, before, countPosts())
				return
			}
			require.NoError(t, err, stderr)
			body := jsonObject(t, lastRequest(t, s.apiHandler, "POST", c.wantPath).Body)
			for k, v := range c.wantBody {
				assert.Equal(t, v, body[k], k)
			}
		})
	}
}

func TestVariableUpdateOptions(t *testing.T) {
	t.Parallel()
	s := setupVariableTest(t)
	s.mainEnv.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{"app": {Name: "app", Type: "golang:1.23"}},
		Routes:  map[string]any{},
		Links:   mockapi.MakeHALLinks("self=/projects/" + s.projectID + "/environments/main/deployment/current"),
	})
	s.apiHandler.SetEnvironments([]*mockapi.Environment{s.mainEnv})
	f, p := s.factory, s.projectID

	f.Run("variable:create", "-p", p, "-l", "p", "env:PROJ", "--value", "v")
	f.Run("variable:create", "-p", p, "-l", "e", "-e", "main", "env:ENV", "--value", "v", "--no-wait")

	cases := []struct {
		name      string
		args      []string
		wantPatch map[string]any
		wantErr   string
		wantCode  int
	}{
		{
			name:      "environment level",
			args:      []string{"-l", "e", "-e", "main", "env:ENV", "--enabled", "false", "--inheritable", "no", "--visible-build", "yes"},
			wantPatch: map[string]any{"is_enabled": false, "is_inheritable": false, "visible_build": true},
		},
		{
			name:     "no change",
			args:     []string{"-l", "e", "-e", "main", "env:ENV", "--enabled", "false"},
			wantErr:  "No changes were provided.",
			wantCode: 1,
		},
		{
			name:    "no change allowed",
			args:    []string{"-l", "e", "-e", "main", "env:ENV", "--enabled", "false", "--allow-no-change"},
			wantErr: "No changes were provided.",
		},
		{
			name:     "enabled at the project level",
			args:     []string{"-l", "p", "env:PROJ", "--enabled", "false"},
			wantErr:  "The option --enabled can only be used for variables at the environment level, not at the project level.",
			wantCode: 1,
		},
		{
			name:     "inheritable at the project level",
			args:     []string{"-l", "p", "env:PROJ", "--inheritable", "false"},
			wantErr:  "The option --inheritable can only be used for variables at the environment level, not at the project level.",
			wantCode: 1,
		},
		{
			name:     "invalid boolean",
			args:     []string{"-l", "p", "env:PROJ", "--visible-build", "maybe"},
			wantErr:  "Invalid value for 'Visible at build time': maybe",
			wantCode: 1,
		},
		{
			name:     "unknown app scope",
			args:     []string{"-l", "p", "env:PROJ", "--app-scope", "nonexistent"},
			wantErr:  `The app, worker, or task "nonexistent" was not found. Valid names are: app`,
			wantCode: 1,
		},
		{
			name:      "app scope",
			args:      []string{"-l", "p", "env:PROJ", "--app-scope", "app"},
			wantPatch: map[string]any{"application_scope": []any{"app"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			before := len(s.apiHandler.AllRequests())
			args := append([]string{"variable:update", "-p", p, "--no-wait"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			var patches []mockapi.Request
			for _, r := range s.apiHandler.AllRequests()[before:] {
				if r.Method == "PATCH" {
					patches = append(patches, r)
				}
			}
			if c.wantErr != "" {
				if c.wantCode == 0 {
					assert.NoError(t, err)
				} else {
					assertExitCode(t, c.wantCode, err)
				}
				assert.Contains(t, stderr, c.wantErr)
				assert.Empty(t, patches)
				return
			}
			require.NoError(t, err, stderr)
			require.Len(t, patches, 1)
			assert.Equal(t, c.wantPatch, jsonObject(t, patches[0].Body))
		})
	}
	assertTrimmed(t, "false", f.Run("variable:get", "-p", p, "-l", "e", "-e", "main", "env:ENV", "-P", "is_enabled"))
}
