package commands

import (
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"

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
			cmd:  innerAppConfigValidateCommand(cnf),
			expected: `Examples:
 Validate the project configuration files in the current Git repository:
   upsun lint

 Validate the project configuration files in a specific directory:
   upsun lint path/to/project

 Validate the current Git repository and output the results as JSON:
   upsun lint --format=json
`,
		},
		{
			name: "init",
			cmd:  innerProjectInitCommand(cnf),
			expected: `Examples:
 Create the starter YAML file(s) for your project:
   upsun init

 Disable AI mode:
   upsun init --ai=false
`,
		},
		{
			name: "convert",
			cmd:  innerProjectConvertCommand(cnf),
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
