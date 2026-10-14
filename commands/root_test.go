package commands

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal"
	"github.com/upsun/cli/internal/config"
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

func TestPrintUpdateMessageWithAltConfig(t *testing.T) {
	cnf, err := config.FromYAML([]byte(`
application:
  {name: Example CLI, slug: example-cli, executable: example, env_prefix: EXAMPLE_CLI_, user_config_dir: .example-cli}
service: {name: Example, env_prefix: EXAMPLE_, project_config_dir: .example}
api: {base_url: "https://api.example.com", auth_url: "https://auth.example.com"}
detection: {git_remote_name: example, site_domains: [example.site]}
ssh: {domain_wildcards: ["*.example.site"]}
`))
	require.NoError(t, err)
	t.Setenv("EXAMPLE_CLI_INSTALL_METHOD", "homebrew")

	buf := &bytes.Buffer{}
	printUpdateMessage(buf, &internal.ReleaseInfo{Version: "9.9.9", URL: "https://example.com/release"}, cnf)
	bin := cnf.Embedded()
	assert.Contains(t, buf.String(), "A new release of the "+bin.Application.Name+" is available")
	if bin.Wrapper.HomebrewTap != "" {
		assert.Contains(t, buf.String(), "brew update && brew upgrade "+bin.Wrapper.HomebrewTap)
	}
}
