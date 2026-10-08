package commands

import (
	"testing"

	"github.com/platformsh/platformify/vendorization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthCommands_Disabled(t *testing.T) {
	t.Setenv("TEST_GO_AUTH", "1")
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
	t.Setenv("TEST_GO_AUTH", "1")
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

func TestAuthCommands_GoAuthDisabled(t *testing.T) {
	t.Setenv("TEST_GO_AUTH", "")
	assert.Empty(t, authCommands(testConfig()))
	root := newRootCommand(testConfig(), &vendorization.VendorAssets{})
	c, _, err := root.Find([]string{"auth:internal"})
	require.NoError(t, err)
	assert.Equal(t, root, c, "auth:internal must not be registered")
}

func TestBrowserCommand_Whitespace(t *testing.T) {
	assert.Nil(t, browserCommand("0"))
	assert.NotPanics(t, func() { browserCommand("  ") })
}
