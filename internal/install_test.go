package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
				getenv: func(string) string { return "" }, fileExists: noFile, brewPrefix: noBrew,
			},
			want: InstallHomebrew,
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
