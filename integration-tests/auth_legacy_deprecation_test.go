package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacyAuthDeprecation checks that a warning is shown when legacy auth is enabled.
func TestLegacyAuthDeprecation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		goAuth string
		args   []string
		warn   bool
	}{
		{name: "go auth", goAuth: "1", args: []string{"list"}},
		{name: "legacy auth", goAuth: "0", args: []string{"list"}, warn: true},
		{name: "legacy auth, quiet", goAuth: "0", args: []string{"list", "--quiet"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newCommandFactory(t, "", "")
			f.extraEnv = append(f.extraEnv, EnvPrefix+"GO_AUTH="+c.goAuth)
			_, stdErr, err := f.RunCombinedOutput(c.args...)
			require.NoError(t, err)
			if c.warn {
				assert.Contains(t, stdErr, "TEST_CLI_GO_AUTH=0 is deprecated")
			} else {
				assert.NotContains(t, stdErr, "deprecated")
			}
		})
	}
}
