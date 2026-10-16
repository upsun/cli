package commands

import (
	"encoding/json"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
)

func TestHelpPageExamples(t *testing.T) {
	color.NoColor = true
	cnf := &config.Config{}
	cnf.Application.Executable = "upsun"

	cases := []struct {
		name     string
		cmd      Command
		expected string
	}{
		{
			name: "lint",
			cmd:  commandFromCobra(cnf, newLintCommand(cnf)),
			expected: `Examples:
 Validate the project configuration files in the current Git repository (or directory):
   upsun lint

 Validate the project configuration files in a specific directory:
   upsun lint path/to/project

 Validate the current Git repository (or directory) and output the results as JSON:
   upsun lint --format=json
`,
		},
		{
			name: "init",
			cmd:  commandFromCobra(cnf, newInitCommand(cnf, nil)),
			expected: `Examples:
 Create the starter YAML file(s) for your project:
   upsun init

 Disable AI mode:
   upsun init --ai=false
`,
		},
		{
			name: "convert",
			cmd:  commandFromCobra(cnf, newProjectConvertCommand(cnf)),
			expected: `Examples:
 Convert the Platform.sh project configuration files in your current directory:
   upsun convert --provider=platformsh
`,
		},
		{
			name: "no aliases",
			cmd: Command{
				Name:     CommandName{Namespace: "foo", Command: "bar"},
				Usage:    []string{"upsun foo:bar <arg>"},
				Examples: []Example{{Commandline: "x", Description: "Run it"}},
			},
			expected: `Examples:
 Run it:
   upsun foo:bar x
`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Contains(t, c.cmd.HelpPage(cnf), c.expected)
		})
	}
}

func TestCommandFromCobraArguments(t *testing.T) {
	cnf := &config.Config{}
	cnf.Application.Executable = "upsun"
	c := &cobra.Command{
		Use:         "foo:bar <id> [path] [files...]",
		Aliases:     []string{"fb"},
		Annotations: map[string]string{argAnnotationPrefix + "id": "The ID"},
	}
	cmd := commandFromCobra(cnf, c)
	assert.Equal(t, orderedList[Argument]{
		{Name: "id", IsRequired: true, Description: "The ID"},
		{Name: "path"},
		{Name: "files", IsArray: true},
	}, cmd.Definition.Arguments)
	assert.Equal(t, []string{"upsun fb <id> [<path>] [<files>...]"}, cmd.Usage)
}

func TestOrderedListJSON(t *testing.T) {
	cases := []struct {
		json     string
		expected orderedList[Argument]
	}{
		{`[]`, nil},
		{`{}`, nil},
		{
			`{"b":{"name":"b"},"a":{"name":"a","is_required":true}}`,
			orderedList[Argument]{{Name: "b"}, {Name: "a", IsRequired: true}},
		},
	}
	for _, c := range cases {
		t.Run(c.json, func(t *testing.T) {
			var l orderedList[Argument]
			require.NoError(t, json.Unmarshal([]byte(c.json), &l))
			assert.Equal(t, c.expected, l)
		})
	}

	b, err := json.Marshal(orderedList[Argument]{{Name: "b"}, {Name: "a"}})
	require.NoError(t, err)
	assert.Equal(t, `{"b":{"name":"b","is_required":false,"is_array":false,"description":"","default":null},`+
		`"a":{"name":"a","is_required":false,"is_array":false,"description":"","default":null}}`, string(b))
}
