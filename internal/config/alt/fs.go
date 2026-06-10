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

// var so tests can stub os.Executable.
var executableFn = os.Executable

// FindConfigDir finds an appropriate destination directory for an "alt" CLI configuration YAML file.
//
// XDG_CONFIG_HOME is honored explicitly because os.UserConfigDir ignores it on macOS and Windows.
// Per the XDG Base Directory spec, an explicitly set value is honored regardless of whether the
// directory already exists — writeFile creates parents as needed.
func FindConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, subDir), nil
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

// FindBinDir picks a bin directory from a per-OS allowlist. It prefers the allowlist entry that
// already holds the running executable (so the alt installs alongside its source binary in
// package-manager layouts), falling back to the first allowlist entry that is on PATH and
// writable, then ~/.platform-alt/bin.
func FindBinDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}

	candidates := binDirAllowlist(runtime.GOOS, homeDir)
	pathValue := os.Getenv("PATH")
	matchExe := exeMatcher(homeDir)

	var firstValid string
	for _, c := range candidates {
		if !inPathValue(c, pathValue) || !isWritableDir(c) {
			continue
		}
		if matchExe(c) {
			return c, nil
		}
		if firstValid == "" {
			firstValid = c
		}
	}

	if firstValid != "" {
		return firstValid, nil
	}
	return filepath.Join(homeDir, homeSubDir, "bin"), nil
}

// exeMatcher returns a predicate that reports whether a candidate bin directory holds the
// running executable. The symlink branch handles bin directories injected via XDG_BIN_HOME
// that contain a symlink into a separate install location (e.g. a Cellar-style layout): the
// candidate's path doesn't match os.Executable directly, but the symlinked entry resolves to
// the same file.
func exeMatcher(homeDir string) func(string) bool {
	exe, err := executableFn()
	if err != nil {
		return func(string) bool { return false }
	}
	normExeDir := normalizePathEntry(filepath.Dir(exe), homeDir)
	exeBase := filepath.Base(exe)
	resolvedExe, resolveErr := filepath.EvalSymlinks(exe)

	return func(c string) bool {
		if normalizePathEntry(c, homeDir) == normExeDir {
			return true
		}
		if resolveErr != nil {
			return false
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(c, exeBase))
		return err == nil && resolved == resolvedExe
	}
}

func binDirAllowlist(goos, homeDir string) []string {
	// An explicitly set XDG_BIN_HOME is the strongest user signal, so it comes first on every OS.
	xdgBinHome := os.Getenv("XDG_BIN_HOME")

	// Homebrew bin directories (/opt/homebrew/bin on macOS, /home/linuxbrew/.linuxbrew/bin on
	// Linux) are deliberately omitted: those directories are managed by Homebrew, and we should
	// not deposit binaries there.
	var raw []string
	switch goos {
	case "darwin":
		raw = []string{
			xdgBinHome,
			"/usr/local/bin",
			filepath.Join(homeDir, ".local", "bin"),
			filepath.Join(homeDir, "bin"),
		}
	case "windows":
		raw = []string{
			xdgBinHome,
			filepath.Join(homeDir, "scoop", "shims"),
			filepath.Join(homeDir, "AppData", "Local", "Programs"),
			filepath.Join(homeDir, ".local", "bin"),
		}
	default:
		raw = []string{
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
