package internal

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/upsun/cli/internal/config"
)

// InstallMethod identifies how the CLI binary was installed, so update messages
// can be tailored or suppressed.
type InstallMethod string

const (
	InstallUnknown  InstallMethod = ""
	InstallHomebrew InstallMethod = "homebrew"
	InstallScoop    InstallMethod = "scoop"
	InstallNpm      InstallMethod = "npm"
	InstallPackage  InstallMethod = "package" // a system package manager: apt, yum/dnf, apk
	InstallScript   InstallMethod = "script"  // the bash installer's raw method
)

// AutoUpdating reports whether the host system updates the CLI on its own, in
// which case the CLI should stay quiet about new releases.
func (m InstallMethod) AutoUpdating() bool {
	return m == InstallPackage
}

// installProbe holds the (injectable) inputs to install-method detection.
type installProbe struct {
	goos         string
	exe          string // the resolved (symlinks followed) executable path
	envPrefix    string
	slug         string
	configMethod string
	brewFormula  string // the last segment of the Homebrew tap, e.g. "upsun-cli"
	getenv       func(string) string
	fileExists   func(string) bool
	brewPrefix   func() (string, bool)
	pkgOwned     func(exe string) bool
}

var (
	detectOnce     sync.Once
	detectedMethod InstallMethod
)

// DetectInstallMethod resolves the install method once per process. It may shell
// out to "brew", so it is only called when about to print an update message,
// never on the hot path of every command.
func DetectInstallMethod(cnf *config.Config) InstallMethod {
	detectOnce.Do(func() {
		detectedMethod = detectInstallMethod(&installProbe{
			goos:         runtime.GOOS,
			exe:          resolvedExecutable(),
			envPrefix:    cnf.Application.EnvPrefix,
			slug:         cnf.Application.Slug,
			configMethod: cnf.Wrapper.InstallMethod,
			brewFormula:  path.Base(cnf.Wrapper.HomebrewTap),
			getenv:       getenvFunc,
			fileExists:   fileExists,
			brewPrefix:   brewPrefix,
			pkgOwned:     ownedBySystemPackage,
		})
	})
	return detectedMethod
}

// IsAutoUpdating is a cheap check (no subprocess) suitable for the hot path. It
// only considers an explicit override and the package marker file.
func IsAutoUpdating(cnf *config.Config) bool {
	if m, ok := overrideMethod(cnf.Application.EnvPrefix, cnf.Wrapper.InstallMethod, getenvFunc); ok {
		return m.AutoUpdating()
	}
	return packageMarkerExists(resolvedExecutable(), cnf.Application.Slug, fileExists)
}

func detectInstallMethod(p *installProbe) InstallMethod {
	if m, ok := overrideMethod(p.envPrefix, p.configMethod, p.getenv); ok {
		return m
	}
	if packageMarkerExists(p.exe, p.slug, p.fileExists) {
		return InstallPackage
	}
	n := normPath(p.exe)
	if strings.Contains(n, "/node_modules/") {
		return InstallNpm
	}
	if p.goos == "windows" && strings.Contains(n, "/scoop/") {
		return InstallScoop
	}
	if isHomebrew(p.exe, p.brewFormula, p.brewPrefix) {
		return InstallHomebrew
	}
	if inStandardBinDir(p.exe) {
		// Packages installed before the marker file existed lack it.
		if p.goos == "linux" && p.pkgOwned(p.exe) {
			return InstallPackage
		}
		return InstallScript
	}
	return InstallUnknown
}

// overrideMethod reads a forced install method from the environment or config.
func overrideMethod(envPrefix, configMethod string, getenv func(string) string) (InstallMethod, bool) {
	if v := getenv(envPrefix + "INSTALL_METHOD"); v != "" {
		return parseMethod(v)
	}
	if configMethod != "" {
		return parseMethod(configMethod)
	}
	return InstallUnknown, false
}

func parseMethod(v string) (InstallMethod, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case string(InstallHomebrew), "brew":
		return InstallHomebrew, true
	case string(InstallScoop):
		return InstallScoop, true
	case string(InstallNpm):
		return InstallNpm, true
	case string(InstallScript):
		return InstallScript, true
	case string(InstallPackage), "apt", "yum", "dnf", "apk", "deb", "rpm":
		return InstallPackage, true
	}
	return InstallUnknown, false
}

// packageMarkerExists reports whether the system package installed a marker file.
// Packages install the binary at <prefix>/bin/<exe>; the marker sits alongside at
// <prefix>/share/<slug>/install-source.
func packageMarkerExists(exe, slug string, fileExists func(string) bool) bool {
	if exe == "" || slug == "" {
		return false
	}
	prefix := filepath.Dir(filepath.Dir(exe))
	return fileExists(filepath.Join(prefix, "share", slug, "install-source"))
}

func isHomebrew(exe, formula string, brewPrefix func() (string, bool)) bool {
	n := normPath(exe)
	// Homebrew bins symlink into the Cellar; the resolved path reveals it cheaply.
	if formula != "" && formula != "." && strings.Contains(n, "/cellar/"+strings.ToLower(formula)+"/") {
		return true
	}
	if prefix, ok := brewPrefix(); ok && prefix != "" {
		if strings.HasPrefix(n, normPath(prefix)+"/bin/") {
			return true
		}
	}
	return false
}

func inStandardBinDir(exe string) bool {
	if exe == "" {
		return false
	}
	dir := strings.TrimSuffix(normPath(filepath.Dir(exe)), "/")
	switch dir {
	case "/usr/bin", "/usr/local/bin", "/bin", "/usr/sbin", "/sbin", "/opt/bin":
		return true
	}
	return strings.HasSuffix(dir, "/.local/bin")
}

// normPath lowercases and normalizes separators so matching works regardless of
// the host OS (useful for cross-platform tests and Windows paths).
func normPath(p string) string {
	return strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
}

var (
	resolvedExeOnce sync.Once
	resolvedExe     string
)

// resolvedExecutable returns the current executable with symlinks followed, so
// Homebrew/Scoop shims resolve to their real location. Memoized.
func resolvedExecutable() string {
	resolvedExeOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		resolvedExe = exe
	})
	return resolvedExe
}

func getenvFunc(k string) string {
	return os.Getenv(k)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ownedBySystemPackage asks the system package database whether it owns exe.
func ownedBySystemPackage(exe string) bool {
	queries := [][]string{
		{"dpkg-query", "-S", exe},
		{"rpm", "-qf", exe},
		{"apk", "info", "--who-owns", exe},
	}
	for _, q := range queries {
		if p, err := exec.LookPath(q[0]); err == nil && exec.Command(p, q[1:]...).Run() == nil {
			return true
		}
	}
	return false
}

func brewPrefix() (string, bool) {
	exe, err := exec.LookPath("brew")
	if err != nil {
		return "", false
	}
	out, err := exec.Command(exe, "--prefix").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
