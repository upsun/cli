package commands

import (
	"io"
	"testing"

	"github.com/platformsh/platformify/vendorization"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompletionScriptShellOption checks that the generated completion scripts
// request suggestions with the long --shell option, instead of the glued short
// option that Symfony's templates use.
func TestCompletionScriptShellOption(t *testing.T) {
	cases := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "zsh",
			template: `requestComp="${words[0]} ${words[1]} _complete --no-interaction -szsh -a1 -c$((CURRENT-1))" i=""`,
			expected: `requestComp="${words[0]} ${words[1]} _complete --no-interaction --shell=zsh -a1 -c$((CURRENT-1))" i=""`,
		},
		{
			name:     "bash",
			template: `local completecmd=("$sf_cmd" "_complete" "--no-interaction" "-sbash" "-c$cword" "-a1")`,
			expected: `local completecmd=("$sf_cmd" "_complete" "--no-interaction" "--shell=bash" "-c$cword" "-a1")`,
		},
		{
			name:     "fish",
			template: `set completecmd "$sf_cmd[1]" "_complete" "--no-interaction" "-sfish" "-a1"`,
			expected: `set completecmd "$sf_cmd[1]" "_complete" "--no-interaction" "--shell=fish" "-a1"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.expected, shellOptionReplacer.Replace(c.template))
		})
	}
}

// TestCompleteCommandPassesArgsThrough checks that the arguments of a
// completion request reach the legacy CLI unchanged. Cobra used to split the
// glued -s<shell> option into single-letter flags, and the bundled -h made it
// print the help page instead of any completions.
func TestCompleteCommandPassesArgsThrough(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			name: "zsh",
			args: []string{"--no-interaction", "-szsh", "-a1", "-c1", "-itest-cli-executable", "-ienv"},
		},
		{
			name: "bash",
			args: []string{"--no-interaction", "-sbash", "-c1", "-a1", "-itest-cli-executable", "-ienv"},
		},
		{
			name: "fish",
			args: []string{"--no-interaction", "-sfish", "-a1", "-itest-cli-executable", "-ienv"},
		},
		{
			name: "long options",
			args: []string{"--no-interaction", "--shell=zsh", "--api-version=1", "--current=1", "--input=test-cli-executable"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cnf := testConfig()
			root := newRootCommand(cnf, &vendorization.VendorAssets{Binary: cnf.Application.Executable})
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)

			var helpCalled bool
			root.SetHelpFunc(func(_ *cobra.Command, _ []string) { helpCalled = true })

			// Stub out the command so that the legacy CLI is not executed.
			completeCmd, _, err := root.Find([]string{completeCommandName})
			require.NoError(t, err)
			require.Equal(t, completeCommandName, completeCmd.Name())

			var got []string
			completeCmd.Run = func(_ *cobra.Command, args []string) { got = args }

			root.SetArgs(append([]string{completeCommandName}, c.args...))
			require.NoError(t, root.Execute())

			assert.False(t, helpCalled, "the help page was printed instead of completions")
			assert.Equal(t, c.args, got)
		})
	}
}
