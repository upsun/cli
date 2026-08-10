package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal"
)

func TestUpgradeCommandFor(t *testing.T) {
	cnf := testConfig()
	cnf.Wrapper.HomebrewTap = "upsun/tap/upsun-cli"
	cnf.Wrapper.NpmPackage = "upsun"
	cnf.Wrapper.InstallerURL = "https://example.com/installer.sh"
	cnf.Application.Executable = "upsun"

	cases := []struct {
		method internal.InstallMethod
		want   string
	}{
		{internal.InstallHomebrew, "brew update && brew upgrade upsun/tap/upsun-cli"},
		{internal.InstallScoop, "scoop update upsun"},
		{internal.InstallNpm, "npm install -g upsun@latest"},
		{internal.InstallScript, "curl -fsSL https://example.com/installer.sh | bash"},
		{internal.InstallPackage, ""}, // suppressed; no tailored command
		{internal.InstallUnknown, ""}, // falls back to the generic link
	}
	for _, c := range cases {
		t.Run(string(c.method), func(t *testing.T) {
			assert.Equal(t, c.want, upgradeCommandFor(cnf, c.method))
		})
	}
}

func TestUpgradeCommandForMissingConfigFallsBack(t *testing.T) {
	cnf := testConfig() // no Wrapper.* fields set
	// Homebrew/npm/script require their config field; without it, fall back (empty).
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallHomebrew))
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallNpm))
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallScript))
}
