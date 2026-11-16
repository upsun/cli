package internal

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
	InstallWinget   InstallMethod = "winget"
	InstallApt      InstallMethod = "apt"
	InstallDnf      InstallMethod = "dnf"
	InstallYum      InstallMethod = "yum"
	InstallApk      InstallMethod = "apk"
	InstallPackage  InstallMethod = "package" // a system package, with an unknown package manager
	InstallScript   InstallMethod = "script"  // the bash installer's raw method
)

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
	pkgManager   func(exe string) InstallMethod
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
		detectedMethod = detectInstallMethod(newInstallProbe(cnf))
	})
	return detectedMethod
}

// newInstallProbe describes the binary from its embedded config, even if
// CLI_CONFIG_FILE is set, apart from the environment variable prefix.
func newInstallProbe(cnf *config.Config) *installProbe {
	bin := cnf.Embedded()
	return &installProbe{
		goos:         runtime.GOOS,
		exe:          resolvedExecutable(),
		envPrefix:    cnf.Application.EnvPrefix,
		slug:         bin.Application.Slug,
		configMethod: bin.Wrapper.InstallMethod,
		brewFormula:  path.Base(bin.Wrapper.HomebrewTap),
		getenv:       getenvFunc,
		fileExists:   fileExists,
		brewPrefix:   brewPrefix,
		pkgManager:   owningPackageManager,
	}
}

// IsWinget is a cheap check (no subprocess) for a winget install.
func IsWinget(cnf *config.Config) bool {
	if m, ok := overrideMethod(cnf.Application.EnvPrefix, cnf.Embedded().Wrapper.InstallMethod, getenvFunc); ok {
		return m == InstallWinget
	}
	return isWingetPath(runtime.GOOS, resolvedExecutable())
}

// isWingetPath reports whether exe is in a winget portable package directory,
// e.g. %LOCALAPPDATA%\Microsoft\WinGet\Packages or %ProgramFiles%\WinGet\Packages.
func isWingetPath(goos, exe string) bool {
	return goos == "windows" && strings.Contains(normPath(exe), "/winget/packages/")
}

func detectInstallMethod(p *installProbe) InstallMethod {
	if m, ok := overrideMethod(p.envPrefix, p.configMethod, p.getenv); ok {
		return m
	}
	if packageMarkerExists(p.exe, p.slug, p.fileExists) {
		if m := p.pkgManager(p.exe); m != InstallUnknown {
			return m
		}
		return InstallPackage
	}
	n := normPath(p.exe)
	if strings.Contains(n, "/node_modules/") {
		return InstallNpm
	}
	if isWingetPath(p.goos, p.exe) {
		return InstallWinget
	}
	if p.goos == "windows" && strings.Contains(n, "/scoop/") {
		return InstallScoop
	}
	if isHomebrew(p.exe, p.brewFormula, p.brewPrefix) {
		return InstallHomebrew
	}
	if inStandardBinDir(p.exe) {
		// Packages installed before the marker file existed lack it.
		if p.goos == "linux" {
			if m := p.pkgManager(p.exe); m != InstallUnknown {
				return m
			}
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
	case string(InstallWinget):
		return InstallWinget, true
	case string(InstallScript):
		return InstallScript, true
	case string(InstallApt):
		return InstallApt, true
	case string(InstallDnf):
		return InstallDnf, true
	case string(InstallYum):
		return InstallYum, true
	case string(InstallApk):
		return InstallApk, true
	case string(InstallPackage), "deb", "rpm":
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

// owningPackageManager asks the system package databases which one owns exe,
// and returns its install method, or InstallUnknown if none does. A time limit
// stops a slow database from delaying the command; a timeout counts as owned
// (InstallPackage), so that the installer script is not suggested.
func owningPackageManager(exe string) InstallMethod {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queries := []struct {
		method InstallMethod
		args   []string
	}{
		{InstallApt, []string{"dpkg-query", "-S", exe}},
		{InstallDnf, []string{"rpm", "-qf", exe}},
		{InstallApk, []string{"apk", "info", "--who-owns", exe}},
	}
	for _, q := range queries {
		p, err := exec.LookPath(q.args[0])
		if err != nil {
			continue
		}
		err = exec.CommandContext(ctx, p, q.args[1:]...).Run()
		if ctx.Err() != nil {
			return InstallPackage
		}
		if err != nil {
			continue
		}
		if q.method == InstallDnf {
			return rpmFrontend(exec.LookPath)
		}
		return q.method
	}
	return InstallUnknown
}

// rpmFrontend returns the install method for an RPM package: dnf, or else yum.
func rpmFrontend(lookPath func(string) (string, error)) InstallMethod {
	for _, m := range []InstallMethod{InstallDnf, InstallYum} {
		if _, err := lookPath(string(m)); err == nil {
			return m
		}
	}
	return InstallPackage
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
