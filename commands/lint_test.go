package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/pkg/lint"
)

// The JSON output documents "errors" and "warnings" as arrays, so a valid
// configuration must still yield arrays rather than null.
func TestPrintLintResult_JSONAlwaysArrays(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	require.NoError(t, printLintResult(cmd, &lint.Result{}, "json"))

	// Pointers distinguish a JSON null from an empty array.
	var decoded struct {
		Errors   *[]lint.Issue `json:"errors"`
		Warnings *[]lint.Issue `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	require.NotNil(t, decoded.Errors, "errors should be an array, not null")
	require.NotNil(t, decoded.Warnings, "warnings should be an array, not null")
	assert.Empty(t, *decoded.Errors)
	assert.Empty(t, *decoded.Warnings)
}

func TestPrintLintResult_JSONWithIssues(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	result := &lint.Result{
		Errors:   []lint.Issue{{Path: "applications.app1.type", Message: "invalid type"}},
		Warnings: []lint.Issue{{Path: "applications.app1.web.commands.start", Message: "a start command is needed"}},
	}

	require.ErrorIs(t, printLintResult(cmd, result, "json"), errLintFailed)

	var decoded struct {
		Errors   []lint.Issue `json:"errors"`
		Warnings []lint.Issue `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	assert.Equal(t, []lint.Issue{{Path: "applications.app1.type", Message: "invalid type"}}, decoded.Errors)
	assert.Equal(t, []lint.Issue{
		{Path: "applications.app1.web.commands.start", Message: "a start command is needed"},
	}, decoded.Warnings)
}

//nolint:lll
func TestPrintLintResult_Text(t *testing.T) {
	color.NoColor = true
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	result := &lint.Result{}
	result.Errors = []lint.Issue{
		{File: ".upsun/config.yaml", Line: 632, Path: "applications.b.mounts.m.source", Message: "must be one of the following"},
		{File: ".upsun/config.yaml", Line: 28, Path: "applications.a.type", Message: "type not found"},
		{Path: "applications", Message: "no application configuration found"},
		{File: ".upsun/tasks.yaml", Message: "unknown top-level key"},
	}
	result.Warnings = []lint.Issue{
		{File: "sub/.upsun", Message: "this .upsun directory is not at the project root and will be ignored"},
	}

	require.ErrorIs(t, printLintResult(cmd, result, "text"), errLintFailed)
	assert.Equal(t, `Errors:
  applications
    no application configuration found
  .upsun/config.yaml
     28  applications.a.type
         type not found
    632  applications.b.mounts.m.source
         must be one of the following
  .upsun/tasks.yaml
    unknown top-level key
Warnings:
  sub/.upsun
    this .upsun directory is not at the project root and will be ignored
`, out.String())
}

func TestLintCommand(t *testing.T) {
	color.NoColor = true
	valid := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(valid, ".platform.app.yaml"),
		[]byte("name: app\ntype: \"php:8.4\"\n"), 0o600))
	missing := filepath.Join(t.TempDir(), "missing")

	cases := []struct {
		name       string
		args       []string
		wantErr    bool
		wantStdout string
	}{
		{"report on stdout", []string{valid}, false, "✓ The configuration is valid.\n"},
		{"stdin with a path", []string{"--stdin", valid}, true, ""},
		{"JSON for an operational error", []string{"--format", "json", missing}, true, `{
  "errors": [
    {
      "path": "",
      "message": "stat ` + missing + `: no such file or directory"
    }
  ],
  "warnings": []
}
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cnf := &config.Config{}
			cnf.Service.ProjectConfigDir = ".platform"
			cmd := newLintCommand(cnf)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader(""))
			cmd.SetArgs(c.args)
			err := cmd.Execute()
			if c.wantErr {
				require.ErrorIs(t, err, errLintFailed)
			} else {
				require.NoError(t, err)
			}
			if c.wantStdout != "" {
				assert.Equal(t, c.wantStdout, stdout.String())
			}
			if c.name == "stdin with a path" {
				assert.Contains(t, stderr.String(), "--stdin cannot be used with a path")
			}
		})
	}
}

func TestPrintLintResult_ValidWithWarnings(t *testing.T) {
	color.NoColor = true
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	result := &lint.Result{Warnings: []lint.Issue{{Message: "both .upsun and .platform configuration found"}}}
	require.NoError(t, printLintResult(cmd, result, "text"))
	assert.Equal(t, `Warnings:
  both .upsun and .platform configuration found
✓ The configuration is valid, with 1 warning.
`, out.String())
}

func TestFindProjectRoot(t *testing.T) {
	gitInit := func(t *testing.T, dir string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	}

	t.Run("ascends to the enclosing git root", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		sub := filepath.Join(root, "services", "api")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		assert.Equal(t, root, findProjectRoot(sub))
	})

	t.Run("nearest git root wins over an outer one", func(t *testing.T) {
		outer := t.TempDir()
		gitInit(t, outer)
		inner := filepath.Join(outer, "vendor", "pkg")
		gitInit(t, inner)
		assert.Equal(t, inner, findProjectRoot(inner))
	})
}

func TestLintCommand_Registration(t *testing.T) {
	cases := []struct {
		name      string
		configDir string
		appFile   string
		want      bool
	}{
		{"upsun", ".upsun", "", true},
		{"platform", ".platform", ".platform.app.yaml", true},
		{"custom config dir", ".acme", "", false},
		{"custom app file", ".platform", ".acme.app.yaml", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cnf := testConfig()
			cnf.Service.ProjectConfigDir = c.configDir
			cnf.Service.AppConfigFile = c.appFile
			root := newRootCommand(cnf)
			cmd, _, err := root.Find([]string{"lint"})
			require.NoError(t, err)
			assert.Equal(t, c.want, cmd != root)
		})
	}
}
