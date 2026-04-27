package alt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

const (
	subDir     = "platform-alt"
	homeSubDir = ".platform-alt"
)

// executableFn is the source of the current executable path. It is a variable so tests can stub
// out os.Executable.
var executableFn = os.Executable

// FindConfigDir finds an appropriate destination directory for an "alt" CLI configuration YAML file.
//
// XDG_CONFIG_HOME takes precedence on all platforms, since os.UserConfigDir does not honor it on
// macOS or Windows. If neither XDG_CONFIG_HOME nor os.UserConfigDir yields an existing directory,
// it falls back to ~/.platform-alt.
func FindConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		isDir, err := isExistingDirectory(xdg)
		if err != nil {
			return "", err
		}
		if isDir {
			return filepath.Join(xdg, subDir), nil
		}
	}

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	isDir, err := isExistingDirectory(userConfigDir)
	if err != nil {
		return "", err
	}
	if isDir {
		return filepath.Join(userConfigDir, subDir), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, homeSubDir), nil
}

// FindBinDir finds an appropriate destination directory for an "alt" CLI executable.
//
// The selection rules are:
//
//  1. If the currently running CLI executable lives in (or is reachable via a symlink from) a
//     directory on the per-OS allowlist, and that directory is writable and on PATH, install
//     the alt there. This co-locates the alt with the source binary in package-manager installs
//     (e.g. Homebrew, Linuxbrew, Scoop).
//  2. Otherwise, pick the first allowlist entry that is writable and on PATH.
//  3. Otherwise, fall back to ~/.platform-alt/bin (the caller is expected to print PATH
//     instructions in that case).
//
// The symlink reachability check matters because os.Executable behaves differently per OS: on
// macOS and Windows it returns the path used to invoke the binary (preserving symlinks), while
// on Linux it returns /proc/self/exe fully resolved. Linuxbrew installs a symlink in
// /home/linuxbrew/.linuxbrew/bin pointing into a versioned Cellar directory, so the resolved
// exe path doesn't match the allowlist directly — we have to compare resolved targets instead.
//
// The allowlist exists to avoid installing alongside binaries in version-scoped or developer
// locations such as ~/.nvm/versions/node/<v>/bin or a local ./dist build directory.
func FindBinDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}

	candidates := binDirAllowlist(homeDir)
	pathValue := os.Getenv("PATH")

	if exe, err := executableFn(); err == nil {
		if dir, ok := findCoLocatedBinDir(exe, candidates, pathValue, homeDir); ok {
			return dir, nil
		}
	}

	for _, c := range candidates {
		if inPathValue(c, pathValue) && isWritableDir(c) {
			return c, nil
		}
	}

	return filepath.Join(homeDir, homeSubDir, "bin"), nil
}

// findCoLocatedBinDir returns the allowlist entry that holds the running executable, either
// directly or via a symlink, provided the entry is writable and on PATH. It handles the
// platform-specific behavior of os.Executable described on FindBinDir.
func findCoLocatedBinDir(exe string, allowlist []string, pathValue, homeDir string) (string, bool) {
	exeDir := filepath.Dir(exe)
	exeBase := filepath.Base(exe)
	resolvedExe, resolveExeErr := filepath.EvalSymlinks(exe)

	for _, c := range allowlist {
		if !inPathValue(c, pathValue) || !isWritableDir(c) {
			continue
		}
		// Direct match: exe's directory equals this allowlist entry. Covers macOS Homebrew
		// (where os.Executable preserves the bin-dir symlink path) and plain copies.
		if normalizePathEntry(exeDir, homeDir) == normalizePathEntry(c, homeDir) {
			return c, true
		}
		// Symlink-resolved match: <c>/<exeBase> resolves to the same file as exe. Covers
		// Linuxbrew, where os.Executable returns the resolved Cellar path but the allowlist
		// entry points at the bin dir that holds the symlink.
		if resolveExeErr != nil {
			continue
		}
		resolvedCandidate, err := filepath.EvalSymlinks(filepath.Join(c, exeBase))
		if err != nil {
			continue
		}
		if resolvedCandidate == resolvedExe {
			return c, true
		}
	}
	return "", false
}

// binDirAllowlist returns the per-OS list of acceptable bin directories, in priority order.
// Empty entries (e.g. unset XDG_BIN_HOME) and duplicates are removed.
func binDirAllowlist(homeDir string) []string {
	xdgBinHome := os.Getenv("XDG_BIN_HOME")

	var raw []string
	switch runtime.GOOS {
	case "darwin":
		raw = []string{
			"/opt/homebrew/bin",
			"/usr/local/bin",
			xdgBinHome,
			filepath.Join(homeDir, ".local", "bin"),
			filepath.Join(homeDir, "bin"),
		}
	case "windows":
		raw = []string{
			filepath.Join(homeDir, "scoop", "shims"),
			filepath.Join(homeDir, "AppData", "Local", "Programs"),
			filepath.Join(homeDir, ".local", "bin"),
		}
	default:
		raw = []string{
			"/home/linuxbrew/.linuxbrew/bin",
			xdgBinHome,
			filepath.Join(homeDir, ".local", "bin"),
			filepath.Join(homeDir, "bin"),
		}
	}

	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, p := range raw {
		if p == "" {
			continue
		}
		norm := normalizePathEntry(p, homeDir)
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, p)
	}
	return out
}

// isWritableDir reports whether path is an existing directory that the current process can write
// to. Detection is best-effort: it tries to create and remove a temp file in the directory.
func isWritableDir(path string) bool {
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return false
	}
	f, err := os.CreateTemp(path, ".platform-alt-write-check-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// isExistingDirectory checks if a path exists and is a directory.
func isExistingDirectory(path string) (bool, error) {
	stat, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return stat.IsDir(), nil
}

// writeFile creates or overwrites a file.
// If dirMode is not 0, the containing directory will be created, if it does not already exist.
func writeFile(path string, content []byte, dirMode, fileMode fs.FileMode) error {
	if dirMode != 0 {
		if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
			return err
		}
	}

	tmpFile := path + ".tmp"
	if err := os.WriteFile(tmpFile, content, fileMode); err != nil {
		return err
	}

	return os.Rename(tmpFile, path)
}
