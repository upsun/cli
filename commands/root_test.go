package commands

import (
	"bytes"
	"os/exec"
	"slices"
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
	cnf.Wrapper.WingetPackage = "Upsun.CLI"
	cnf.Wrapper.InstallerURL = "https://x.test/i.sh"
	cnf.Wrapper.PackageName = "upsun-cli"
	cnf.Application.Executable = "upsun"

	origGeteuid, origLookPath := geteuid, lookPath
	t.Cleanup(func() { geteuid, lookPath = origGeteuid, origLookPath })
	geteuid = func() int { return 1000 }
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	cases := []struct {
		method internal.InstallMethod
		want   string
	}{
		{internal.InstallHomebrew, "brew update && brew upgrade upsun/tap/upsun-cli"},
		{internal.InstallScoop, "scoop update upsun"},
		{internal.InstallNpm, "npm install -g upsun@latest"},
		{internal.InstallWinget, "winget upgrade Upsun.CLI"},
		{internal.InstallScript, "curl -fsSL https://x.test/i.sh | INSTALL_METHOD=raw INSTALL_DIR=/usr/local/bin sh"},
		{internal.InstallApt, "sudo apt update && sudo apt install --only-upgrade upsun-cli"},
		{internal.InstallDnf, "sudo dnf upgrade --refresh upsun-cli"},
		{internal.InstallYum, "sudo yum clean expire-cache && sudo yum upgrade upsun-cli"},
		{internal.InstallApk, "sudo apk add --upgrade --update-cache upsun-cli"},
		{internal.InstallPackage, ""}, // an unknown package manager falls back to the generic link
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
	// Homebrew/npm/winget/script require their config field; without it, fall back (empty).
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallHomebrew, "/usr/local/bin/upsun"))
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallNpm, "/usr/local/bin/upsun"))
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallWinget, "/usr/local/bin/upsun"))
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallScript, "/usr/local/bin/upsun"))
	assert.Empty(t, upgradeCommandFor(cnf, internal.InstallApt, "/usr/bin/upsun"))
}

func TestPackageUpgradeCommandPrivilege(t *testing.T) {
	assert.Equal(t, "apt update && apt install --only-upgrade upsun-cli",
		packageUpgradeCommand(internal.InstallApt, "upsun-cli", ""))
	assert.Equal(t, "doas apk add --upgrade --update-cache upsun-cli",
		packageUpgradeCommand(internal.InstallApk, "upsun-cli", "doas"))
}

func TestPrivilegeCommand(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			if slices.Contains(names, name) {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		}
	}
	assert.Empty(t, privilegeCommand(true, has("sudo")))
	assert.Equal(t, "sudo", privilegeCommand(false, has("sudo", "doas")))
	assert.Equal(t, "doas", privilegeCommand(false, has("doas")))
	assert.Equal(t, "sudo", privilegeCommand(false, has()))
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
	bin := cnf.Embedded()
	rel := &internal.ReleaseInfo{Version: "9.9.9", URL: "https://example.com/release"}

	buf := &bytes.Buffer{}
	printUpdateMessage(buf, rel, cnf, internal.InstallScoop)
	assert.Contains(t, buf.String(), "A new release of the "+bin.Application.Name+" is available")
	assert.Contains(t, buf.String(), "scoop update "+bin.Application.Executable)

	// Without an upgrade command, an alt CLI (e.g. with a private copy of the binary) shows nothing.
	buf.Reset()
	printUpdateMessage(buf, rel, cnf, internal.InstallUnknown)
	assert.Empty(t, buf.String())
}

func TestPrintUpdateMessageUnknownMethod(t *testing.T) {
	t.Setenv("CLI_CONFIG_FILE", "")
	b, err := config.LoadYAML()
	require.NoError(t, err)
	cnf, err := config.FromYAML(b)
	require.NoError(t, err)

	buf := &bytes.Buffer{}
	printUpdateMessage(buf, &internal.ReleaseInfo{Version: "9.9.9", URL: "https://example.com/release"}, cnf,
		internal.InstallUnknown)
	assert.Contains(t, buf.String(), "https://github.com/"+cnf.Wrapper.GitHubRepo+"#upgrade")
}
