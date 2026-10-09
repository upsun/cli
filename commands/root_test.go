package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal"
)

func TestUpgradeCommandFor(t *testing.T) {
	cnf := testConfig()
	cnf.Wrapper.HomebrewTap = "upsun/tap/upsun-cli"
	cnf.Wrapper.NpmPackage = "upsun"
	cnf.Wrapper.InstallerURL = "https://x.test/i.sh"
	cnf.Application.Executable = "upsun"

	cases := []struct {
		method internal.InstallMethod
		want   string
	}{
		{internal.InstallHomebrew, "brew update && brew upgrade upsun/tap/upsun-cli"},
		{internal.InstallScoop, "scoop update upsun"},
		{internal.InstallNpm, "npm install -g upsun@latest"},
		{internal.InstallScript, "curl -fsSL https://x.test/i.sh | INSTALL_METHOD=raw INSTALL_DIR=/usr/local/bin sh"},
		{internal.InstallPackage, ""}, // suppressed; no tailored command
		{internal.InstallUnknown, ""}, // falls back to the generic link
	}
	for _, c := range cases {
		t.Run(string(c.method), func(t *testing.T) {
			assert.Equal(t, c.want, upgradeCommandFor(cnf, c.method, "/usr/local/bin/upsun"))
		})
	}
}

func TestUpgradeCommandForMissingConfigFallsBack(t *testing.T) {
	cnf := testConfig() // no Wrapper.* fields set
	// Homebrew/npm/script require their config field; without it, fall back (empty).
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallHomebrew, "/usr/local/bin/upsun"))
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallNpm, "/usr/local/bin/upsun"))
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallScript, "/usr/local/bin/upsun"))
}

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/usr/local/bin", "/usr/local/bin"},
		{"/home/Jane Doe/.local/bin", "'/home/Jane Doe/.local/bin'"},
		{"/tmp/it's", `'/tmp/it'\''s'`},
		{"/tmp/$(id)", "'/tmp/$(id)'"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, shellQuote(c.in))
	}
}

func TestStripDebugFlag(t *testing.T) {
	cases := []struct {
		args      []string
		want      []string
		wantDebug bool
	}{
		{[]string{"cc"}, []string{"cc"}, false},
		{[]string{"cc", "--debug"}, []string{"cc"}, true},
		{[]string{"--debug", "cc", "-v"}, []string{"cc", "-v"}, true},
		{[]string{"cc", "--debug=1"}, []string{"cc"}, true},
		{[]string{"cc", "--debug=false"}, []string{"cc"}, false},
		{[]string{"cc", "--debug=foo"}, []string{"cc", "--debug=foo"}, false},
		{[]string{"ssh", "--", "cmd", "--debug"}, []string{"ssh", "--", "cmd", "--debug"}, false},
		{[]string{"ssh", "--debug", "--", "--debug"}, []string{"ssh", "--", "--debug"}, true},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			got, debug := stripDebugFlag(c.args)
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.wantDebug, debug)
		})
	}
}
