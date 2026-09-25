package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/lint"
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

	result := &lint.Result{}
	result.AddError("applications.app1.type", "invalid type")
	result.AddWarning("applications.app1.web.commands.start", "a start command is needed")

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
	cmd.SetErr(&out)

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
