package commands

import (
	"testing"

	"github.com/platformsh/platformify/vendorization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthCommands_Disabled(t *testing.T) {
	cnf := testConfig()
	cnf.Application.DisabledCommands = []string{"auth:api-token-login"}
	cnf.Application.WrappedDisabledCommands = []string{"auth:token"}
	names := make([]string, 0, 2)
	for _, c := range authCommands(cnf) {
		names = append(names, c.Name())
	}
	assert.Equal(t, []string{"auth:browser-login", "auth:logout"}, names)
}

func TestAuthCommands_LegacyGlobalFlags(t *testing.T) {
	root := newRootCommand(testConfig(), &vendorization.VendorAssets{})
	for _, args := range [][]string{
		{"logout", "-n"},
		{"auth:token", "-W", "--no-ansi"},
		{"login", "--ansi", "--no"},
	} {
		c, rest, err := root.Find(args)
		require.NoError(t, err)
		assert.NoError(t, c.ParseFlags(rest), "args: %v", args)
	}
}

func TestBrowserCommand_Whitespace(t *testing.T) {
	assert.Nil(t, browserCommand("0"))
	assert.NotPanics(t, func() { browserCommand("  ") })
}
