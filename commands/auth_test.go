package commands

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestBrowserCommand_WSL(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL is Linux")
	}
	dir := t.TempDir()
	rundll := filepath.Join(dir, "rundll32.exe")
	require.NoError(t, os.WriteFile(rundll, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // an executable stub
	t.Setenv("PATH", dir)
	t.Setenv("DISPLAY", "")
	t.Setenv("WSL_INTEROP", "")
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")

	assert.True(t, isWSL())
	assert.True(t, hasDisplay(), "a Windows browser can be used without a display")
	assert.Equal(t, []string{rundll, "url.dll,FileProtocolHandler"}, browserCommand(""))
	assert.True(t, canOpenURLs(""))
}

func TestIsWSL_NotWSL(t *testing.T) {
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc/WSLInterop"); err == nil {
		t.Skip("running in WSL")
	}
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")
	assert.False(t, isWSL())
}
