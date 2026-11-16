package internal

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
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
			name: "override names a package manager",
			probe: installProbe{
				goos: "linux", exe: "/somewhere/upsun", envPrefix: "UPSUN_CLI_", slug: "upsun",
				getenv:     func(k string) string { return map[string]string{"UPSUN_CLI_INSTALL_METHOD": "apt"}[k] },
				fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallApt,
		},
		{
			name: "package marker present, owned by a package",
			probe: installProbe{
				goos: "linux", exe: "/usr/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, brewPrefix: noBrew,
				fileExists: fileAt("/usr/share/upsun/install-source"),
				pkgManager: func(string) InstallMethod { return InstallApk },
				pkgRepo:    "repo.example.com",
				repoInUse:  func(m InstallMethod, repo string) bool { return m == InstallApk && repo == "repo.example.com" },
			},
			want: InstallApk,
		},
		{
			name: "package marker present, repository not in use (e.g. dpkg -i)",
			probe: installProbe{
				goos: "linux", exe: "/usr/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, brewPrefix: noBrew,
				fileExists: fileAt("/usr/share/upsun/install-source"),
				pkgManager: func(string) InstallMethod { return InstallApt },
				pkgRepo:    "repo.example.com",
				repoInUse:  func(InstallMethod, string) bool { return false },
			},
			want: InstallPackage,
		},
		{
			name: "package marker present, no repository in config",
			probe: installProbe{
				goos: "linux", exe: "/usr/bin/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, brewPrefix: noBrew,
				fileExists: fileAt("/usr/share/upsun/install-source"),
				pkgManager: func(string) InstallMethod { return InstallApt },
			},
			want: InstallPackage,
		},
		{
			name: "package marker present, unknown package manager",
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
			name: "winget user scope, via the resolved link",
			probe: installProbe{
				goos: "windows",
				exe: `C:\Users\u\AppData\Local\Microsoft\WinGet\Packages\` +
					`Upsun.CLI_Microsoft.Winget.Source_8wekyb3d8bbwe\upsun.exe`,
				slug: "upsun", getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallWinget,
		},
		{
			name: "winget machine scope",
			probe: installProbe{
				goos: "windows", exe: `C:\Program Files\WinGet\Packages\Upsun.CLI_Microsoft.Winget.Source_8wekyb3d8bbwe\upsun.exe`,
				slug: "upsun", getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallWinget,
		},
		{
			name: "winget path on linux is not detected as winget",
			probe: installProbe{
				goos: "linux", exe: "/home/u/WinGet/Packages/upsun/upsun", slug: "upsun",
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallUnknown,
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
				pkgManager: func(exe string) InstallMethod {
					if exe == "/usr/bin/upsun" {
						return InstallDnf
					}
					return InstallUnknown
				},
				pkgRepo:   "repo.example.com",
				repoInUse: func(InstallMethod, string) bool { return true },
			},
			want: InstallDnf,
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
			if c.probe.pkgManager == nil {
				c.probe.pkgManager = func(string) InstallMethod { return InstallUnknown }
			}
			assert.Equal(t, c.want, detectInstallMethod(&c.probe))
		})
	}
}

func TestAnyFileEnablesRepo(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	write("other.list", "deb http://deb.example.org stable main\n")
	write("example.list", "deb https://repo.example.com/debian stable main\n")
	write("example.list.distUpgrade", "deb https://backup.example.com/debian stable main\n")

	patterns := []string{filepath.Join(dir, "other.list"), filepath.Join(dir, "*.list")}
	assert.True(t, anyFileEnablesRepo(patterns, "repo.example.com"))
	assert.False(t, anyFileEnablesRepo(patterns, "other.example.com"))
	assert.False(t, anyFileEnablesRepo(patterns, "backup.example.com"))
	assert.False(t, anyFileEnablesRepo(patterns[:1], "repo.example.com"))
	assert.False(t, anyFileEnablesRepo(nil, "repo.example.com"))
}

func TestEnablesRepo(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"apt list", "deb [signed-by=/k.asc] https://repo.example.com/debian stable main\n", true},
		{"apt list disabled by a release upgrade", "# deb https://repo.example.com/debian stable main # disabled\n", false},
		{"deb822", "Types: deb\nURIs: https://repo.example.com/debian\nSuites: stable\n", true},
		{"deb822 disabled", "Types: deb\nURIs: https://repo.example.com/debian\nEnabled: no\n", false},
		{"yum", "[example]\nbaseurl=https://repo.example.com/rpm/$basearch\nenabled=1\ngpgcheck=1\n", true},
		{"yum disabled", "[example]\nbaseurl=https://repo.example.com/rpm/$basearch\nenabled = 0\n", false},
		{"yum disabled with off", "[example]\nbaseurl=https://repo.example.com/rpm/$basearch\nenabled=off\n", false},
		{"deb822 disabled with disable", "URIs: https://repo.example.com/debian\nEnabled: disable\n", false},
		{"deb822 disabled with without", "URIs: https://repo.example.com/debian\nEnabled: without\n", false},
		{"apk", "https://dl-cdn.example.org/alpine/main\nhttps://repo.example.com/alpine\n", true},
		{"apk commented out", "https://dl-cdn.example.org/alpine/main\n  #https://repo.example.com/alpine\n", false},
		{"another repository", "deb http://deb.example.org stable main\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, enablesRepo([]byte(c.content), "repo.example.com"))
		})
	}
}

func TestRPMFrontend(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			if slices.Contains(names, name) {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		}
	}
	assert.Equal(t, InstallDnf, rpmFrontend(has("dnf", "yum")))
	assert.Equal(t, InstallYum, rpmFrontend(has("yum")))
	assert.Equal(t, InstallPackage, rpmFrontend(has("zypper")))
}

func TestParseMethod(t *testing.T) {
	cases := []struct {
		in   string
		want InstallMethod
		ok   bool
	}{
		{"homebrew", InstallHomebrew, true},
		{"brew", InstallHomebrew, true},
		{"APT", InstallApt, true},
		{"dnf", InstallDnf, true},
		{"yum", InstallYum, true},
		{"apk", InstallApk, true},
		{"package", InstallPackage, true},
		{"rpm", InstallPackage, true},
		{"npm", InstallNpm, true},
		{"  scoop  ", InstallScoop, true},
		{"winget", InstallWinget, true},
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
