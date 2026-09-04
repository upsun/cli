package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCompletionScript checks that the generated completion scripts request
// suggestions with the long --shell option, rather than the glued short option
// (-szsh) that Symfony's templates use.
func TestCompletionScript(t *testing.T) {
	f := newCommandFactory(t, "", "")

	cases := []struct {
		shell string
	}{
		{shell: "zsh"},
		{shell: "bash"},
		{shell: "fish"},
	}

	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			script := f.Run("completion", c.shell)
			assert.Contains(t, script, "--shell="+c.shell)
			assert.NotContains(t, script, "-s"+c.shell)
		})
	}
}

// TestComplete checks that a completion request returns suggestions. The glued
// short options of the completion scripts must reach the legacy CLI unparsed:
// the bundled -h of -szsh used to make the CLI print its help page instead.
func TestComplete(t *testing.T) {
	f := newCommandFactory(t, "", "")

	cases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "short options",
			args:     []string{"_complete", "--no-interaction", "-szsh", "-a1", "-c1", "-iplatform-test", "-ienv"},
			expected: "environment:list",
		},
		{
			name: "long options",
			args: []string{
				"_complete", "--no-interaction", "--shell=zsh", "--api-version=1", "--current=1",
				"--input=platform-test", "--input=env",
			},
			expected: "environment:list",
		},
		{
			// An input token can contain an "h" too, as in "platform-test ssh --pro<TAB>".
			name:     "input containing h",
			args:     []string{"_complete", "--no-interaction", "-szsh", "-a1", "-c2", "-iplatform-test", "-issh", "-i--pro"},
			expected: "--project",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Contains(t, f.Run(c.args...), c.expected)
		})
	}
}
