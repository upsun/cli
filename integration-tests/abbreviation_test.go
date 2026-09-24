package tests

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAbbreviation checks that native commands can be abbreviated like legacy ones.
func TestAbbreviation(t *testing.T) {
	f := newCommandFactory(t, "", "")

	cases := []struct {
		args     []string
		expected string
	}{
		{[]string{"p:init", "--help"}, "Command: project:init"},
		{[]string{"help", "p:init"}, "Command: project:init"},
		{[]string{"--help", "p:init"}, "Command: project:init"},
		{[]string{"--help", "init"}, "Command: project:init"},
		{[]string{"a:config-v", "--help"}, "Command: app:config-validate"},
		{[]string{"env:info", "--help"}, "Command: environment:info"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			assert.Contains(t, f.Run(c.args...), c.expected)
		})
	}

	_, stdErr, err := f.RunCombinedOutput("p:c")
	assert.Error(t, err)
	assert.Contains(t, stdErr, `Command "p:c" is ambiguous`)
}
