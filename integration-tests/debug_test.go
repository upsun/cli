package tests

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDebugFlag checks that --debug enables debug logging and is not forwarded to the legacy CLI.
func TestDebugFlag(t *testing.T) {
	f := newCommandFactory(t, "", "")

	cases := []struct {
		args     []string
		expected string
	}{
		{[]string{"cc", "--debug"}, ""},
		{[]string{"--debug", "cc"}, ""},
		{[]string{"cc", "--debug", "--help"}, "Command: clear-cache"},
		{[]string{"--help", "--debug", "cc"}, "Command: clear-cache"},
		{[]string{"help", "cc", "--debug"}, "Command: clear-cache"},
		{[]string{"help", "--debug", "cc"}, "Command: clear-cache"},
		{[]string{"--debug", "p:init", "--help"}, "Command: project:init"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			stdOut, stdErr, err := f.RunCombinedOutput(c.args...)
			require.NoError(t, err, stdErr)
			assert.NotContains(t, stdErr, `"--debug" option does not exist`)
			assert.Contains(t, stdOut, c.expected)
			if c.expected != "Command: project:init" {
				// Logged by the Go wrapper when it starts the legacy CLI.
				assert.Contains(t, stdErr, "Initialized PHP CLI")
			}
		})
	}
}
