package internal

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
)

func TestDetectInstallMethod(t *testing.T) {
	noBrew := func() (string, bool) { return "", false }
	brewAt := func(prefix string) func() (string, bool) {
		return func() (string, bool) { return prefix, true }
	}
	noFile := func(string) bool { return false }
	fileAt := func(want string) func(string) bool {
		return func(p string) bool { return p == want }
	}

	cases := []struct {
		name  string
		probe installProbe
		want  InstallMethod
	}{
		{
			name: "env override wins over path",
			probe: installProbe{
				goos: "linux", exe: "/usr/bin/upsun", envPrefix: "UPSUN_CLI_", slug: "upsun",
				getenv:     func(k string) string { return map[string]string{"UPSUN_CLI_INSTALL_METHOD": "homebrew"}[k] },
				fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallHomebrew,
		},
		{
			name: "config override",
			probe: installProbe{
				goos: "linux", exe: "/usr/bin/upsun", slug: "upsun", configMethod: "npm",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallNpm,
		},
		{
			name: "override alias apt maps to package",
			probe: installProbe{
				goos: "linux", exe: "/somewhere/upsun", envPrefix: "UPSUN_CLI_", slug: "upsun",
				getenv:     func(k string) string { return map[string]string{"UPSUN_CLI_INSTALL_METHOD": "apt"}[k] },
				fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallPackage,
		},
		{
			name: "package marker present",
			probe: installProbe{
				goos: "linux", exe: "/usr/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, brewPrefix: noBrew,
				fileExists: fileAt("/usr/share/upsun/install-source"),
			},
			want: InstallPackage,
		},
		{
			name: "usr-bin without marker is script",
			probe: installProbe{
				goos: "linux", exe: "/usr/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallScript,
		},
		{
			name: "npm node_modules path",
			probe: installProbe{
				goos: "linux", exe: "/home/u/project/node_modules/@upsun/cli-linux-x64/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallNpm,
		},
		{
			name: "scoop path on windows",
			probe: installProbe{
				goos: "windows", exe: `C:\Users\u\scoop\apps\upsun\current\upsun.exe`, slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallScoop,
		},
		{
			name: "scoop path on linux is not detected as scoop",
			probe: installProbe{
				goos: "linux", exe: "/home/u/scoop/apps/upsun/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallUnknown,
		},
		{
			name: "user-local bin is script",
			probe: installProbe{
				goos: "linux", exe: "/home/u/.local/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallScript,
		},
		{
			name: "homebrew via cellar symlink target",
			probe: installProbe{
				goos: "darwin", exe: "/opt/homebrew/Cellar/upsun-cli/2.0.0/bin/upsun", slug: "upsun",
				brewFormula: "upsun-cli",
				getenv:      func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallHomebrew,
		},
		{
			name: "cellar directory not belonging to homebrew",
			probe: installProbe{
				goos: "linux", exe: "/srv/cellar/bin/upsun", slug: "upsun", brewFormula: "upsun-cli",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallUnknown,
		},
		{
			name: "usr-bin owned by a package without marker",
			probe: installProbe{
				goos: "linux", exe: "/usr/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
				pkgOwned: func(exe string) bool { return exe == "/usr/bin/upsun" },
			},
			want: InstallPackage,
		},
		{
			name: "homebrew via brew prefix",
			probe: installProbe{
				goos: "darwin", exe: "/usr/local/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: brewAt("/usr/local"),
			},
			want: InstallHomebrew,
		},
		{
			name: "unknown for go-build temp path",
			probe: installProbe{
				goos: "linux", exe: "/tmp/go-build123/b001/exe/platform", slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallUnknown,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.probe.pkgOwned == nil {
				c.probe.pkgOwned = func(string) bool { return false }
			}
			assert.Equal(t, c.want, detectInstallMethod(&c.probe))
		})
	}
}

func TestInstallMethodAutoUpdating(t *testing.T) {
	assert.True(t, InstallPackage.AutoUpdating())
	assert.False(t, InstallHomebrew.AutoUpdating())
	assert.False(t, InstallScript.AutoUpdating())
	assert.False(t, InstallUnknown.AutoUpdating())
}

func TestParseMethod(t *testing.T) {
	cases := []struct {
		in   string
		want InstallMethod
		ok   bool
	}{
		{"homebrew", InstallHomebrew, true},
		{"brew", InstallHomebrew, true},
		{"APT", InstallPackage, true},
		{"dnf", InstallPackage, true},
		{"apk", InstallPackage, true},
		{"npm", InstallNpm, true},
		{"  scoop  ", InstallScoop, true},
		{"nonsense", InstallUnknown, false},
		{"", InstallUnknown, false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, ok := parseMethod(c.in)
			assert.Equal(t, c.ok, ok)
			if c.ok {
				assert.Equal(t, c.want, got)
			}
		})
	}
}

func TestNewInstallProbeWithAltConfig(t *testing.T) {
	cnf, err := config.FromYAML([]byte(testAltConfig))
	require.NoError(t, err)
	p := newInstallProbe(cnf)
	assert.Equal(t, "EXAMPLE_CLI_", p.envPrefix)
	bin := cnf.Embedded()
	assert.Equal(t, bin.Application.Slug, p.slug)
	assert.Equal(t, path.Base(bin.Wrapper.HomebrewTap), p.brewFormula)
	assert.NotEqual(t, cnf.Application.Slug, p.slug)
}

// testAltConfig is a minimal config, as loaded via CLI_CONFIG_FILE.
const testAltConfig = `
application:
  {name: Example CLI, slug: example-cli, executable: example, env_prefix: EXAMPLE_CLI_, user_config_dir: .example-cli}
service: {name: Example, env_prefix: EXAMPLE_, project_config_dir: .example}
api: {base_url: "https://api.example.com", auth_url: "https://auth.example.com"}
detection: {git_remote_name: example, site_domains: [example.site]}
ssh: {domain_wildcards: ["*.example.site"]}
`

func TestFindScoopMigration(t *testing.T) {
	oldRemote := "[remote \"origin\"]\n\turl = https://github.com/platformsh/homebrew-tap.git\n"
	newRemote := "[remote \"origin\"]\n\turl = https://github.com/upsun/homebrew-tap.git\n"
	otherRemote := "[remote \"origin\"]\n\turl = https://github.com/ScoopInstaller/Main\n"

	cases := []struct {
		name  string
		files map[string]string
		want  *ScoopMigration
	}{
		{
			name: "old bucket",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "platformsh", "architecture": "64bit"}`,
				"buckets/platformsh/.git/config":         oldRemote,
				"buckets/main/.git/config":               otherRemote,
			},
			want: &ScoopMigration{OldBucket: "platformsh"},
		},
		{
			name: "old bucket with a custom name and legacy install.json",
			files: map[string]string{
				"apps/platform/5.9.0/install.json": `{"bucket": "my-tap"}`,
				"buckets/my-tap/.git/config":       strings.ToUpper(oldRemote),
			},
			want: &ScoopMigration{OldBucket: "my-tap"},
		},
		{
			name: "old bucket and an existing new bucket",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "platformsh"}`,
				"buckets/platformsh/.git/config":         oldRemote,
				"buckets/upsun-cli/.git/config":          newRemote,
			},
			want: &ScoopMigration{OldBucket: "platformsh", NewBucket: "upsun-cli"},
		},
		{
			name: "removed bucket",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "platformsh"}`,
				"buckets/main/.git/config":               otherRemote,
			},
			want: &ScoopMigration{},
		},
		{
			name: "removed bucket and an existing new bucket",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "platformsh"}`,
				"buckets/upsun/.git/config":              newRemote,
			},
			want: &ScoopMigration{NewBucket: "upsun"},
		},
		{
			name: "new bucket",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "platformsh"}`,
				"buckets/platformsh/.git/config":         newRemote,
			},
		},
		{
			name: "no buckets directory",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "platformsh"}`,
			},
		},
		{
			name: "unsafe bucket name",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"bucket": "../buckets/platformsh"}`,
				"buckets/platformsh/.git/config":         oldRemote,
			},
		},
		{
			name: "installed from a URL",
			files: map[string]string{
				"apps/platform/5.9.0/scoop-install.json": `{"url": "https://example.com/platform.json"}`,
				"buckets/main/.git/config":               otherRemote,
			},
		},
		{
			name: "no install file",
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
			assert.Equal(t, c.want, FindScoopMigration(exe, "upsun/homebrew-tap"))
		})
	}

	assert.Nil(t, FindScoopMigration("/usr/local/bin/platform", "upsun/homebrew-tap"))
	assert.Nil(t, FindScoopMigration("", "upsun/homebrew-tap"))
}
