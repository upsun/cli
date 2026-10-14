package commands

import (
	"bytes"
	"os"
	"path/filepath"
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
		want   []string
	}{
		{internal.InstallHomebrew, []string{"brew update && brew upgrade upsun/tap/upsun-cli"}},
		{internal.InstallScoop, []string{"scoop update upsun"}},
		{internal.InstallNpm, []string{"npm install -g upsun@latest"}},
		{internal.InstallScript, []string{
			"curl -fsSL https://x.test/i.sh | INSTALL_METHOD=raw INSTALL_DIR=/usr/local/bin sh",
		}},
		{internal.InstallPackage, nil}, // suppressed; no tailored command
		{internal.InstallUnknown, nil}, // falls back to the generic link
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

func TestUpgradeCommandForOldScoopBucket(t *testing.T) {
	oldRemote := "url = https://github.com/platformsh/homebrew-tap.git"
	newRemote := "url = https://github.com/upsun/homebrew-tap.git"
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name: "old bucket",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "platformsh"}`,
				"buckets/platformsh/.git/config":         oldRemote,
			},
			want: []string{
				"scoop bucket rm platformsh",
				"scoop bucket add upsun https://github.com/upsun/homebrew-tap.git",
				"scoop uninstall platform",
				"scoop install upsun/platform",
			},
		},
		{
			name: "removed bucket and an existing new bucket",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "platformsh"}`,
				"buckets/upsun-tap/.git/config":          newRemote,
			},
			want: []string{
				"scoop uninstall platform",
				"scoop install upsun-tap/platform",
			},
		},
		{
			name: "new bucket",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "upsun"}`,
				"buckets/upsun/.git/config":              newRemote,
			},
			want: []string{"scoop update platform"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range c.files {
				p := filepath.Join(root, filepath.FromSlash(name))
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
			}
			exe := filepath.Join(root, "apps", "platform", "5.9.0", "platform.exe")

			cnf := testConfig()
			cnf.Application.Executable = "platform"
			cnf.Wrapper.HomebrewTap = "upsun/tap/platformsh-cli"
			assert.Equal(t, c.want, upgradeCommandFor(cnf, internal.InstallScoop, exe))

			// Without a tap to migrate to, it falls back to scoop update.
			cnf.Wrapper.HomebrewTap = ""
			assert.Equal(t, []string{"scoop update platform"}, upgradeCommandFor(cnf, internal.InstallScoop, exe))
		})
	}
}

func TestPrintUpgradeCommands(t *testing.T) {
	buf := &bytes.Buffer{}
	printUpgradeCommands(buf, []string{"scoop update platform"})
	assert.Equal(t, "To upgrade, run: scoop update platform\n", buf.String())

	buf.Reset()
	printUpgradeCommands(buf, []string{"scoop uninstall platform", "scoop install upsun/platform"})
	assert.Equal(t, "To upgrade, run:\n  scoop uninstall platform\n  scoop install upsun/platform\n", buf.String())
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
