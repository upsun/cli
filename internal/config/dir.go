package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// TempDir returns the path to a user-specific temporary directory, suitable for caches.
//
// It creates the temporary directory if it does not already exist, and checks that it is private to the user.
//
// The directory can be specified in the {ENV_PREFIX}TMP environment variable.
//
// This does not use os.TempDir, as on Linux/Unix systems that usually returns a
// global /tmp directory, which could conflict with other users. It also does not
// use os.MkdirTemp, as the CLI usually needs a stable (not random) directory
// path. It therefore uses os.UserCacheDir which in turn will use XDG_CACHE_HOME
// or the home directory.
func (c *Config) TempDir() (string, error) {
	if c.tempDir != "" {
		return c.tempDir, nil
	}
	d := os.Getenv(c.Application.EnvPrefix + "TMP")
	if d == "" {
		ucd, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		d = ucd
	}

	// Windows already has a user-specific temporary directory.
	if runtime.GOOS == "windows" {
		osTemp := os.TempDir()
		if strings.HasPrefix(osTemp, d) {
			d = osTemp
		}
	}

	path := filepath.Join(d, c.Application.TempSubDir)

	// If the subdirectory cannot be created due to a read-only filesystem, fall back to /tmp.
	// G301: 0o700 restricts to the user; path is the user's own cache dir.
	if err := os.MkdirAll(path, 0o700); err != nil { //nolint:gosec
		if !errors.Is(err, syscall.EROFS) {
			return "", err
		}
		path = filepath.Join(os.TempDir(), c.Application.TempSubDir)
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", err
		}
	}
	if err := ensurePrivateDir(path); err != nil {
		return "", err
	}
	c.tempDir = path

	return path, nil
}

// WritableUserDir returns the path to a writable user-level directory, e.g. for credentials and state.
//
// As in the legacy CLI, which shares it, a temporary directory is used if the directory in the home directory cannot
// be written, e.g. on an application container. The directory must be private to the user.
//
// Deprecated: unless backwards compatibility is desired, TempDir is preferable.
func (c *Config) WritableUserDir() (string, error) {
	if c.writableUserDir != "" {
		return c.writableUserDir, nil
	}
	hd, err := c.HomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(hd, c.Application.WritableUserDir)
	if !canWrite(path) {
		path = filepath.Join(os.TempDir(), c.Application.TempSubDir)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	if err := ensurePrivateDir(path); err != nil {
		return "", err
	}
	c.writableUserDir = path

	return path, nil
}

// canWrite checks whether a directory is writable, or can be created, using permissions only.
//
// This matches the legacy CLI (Filesystem::canWrite), so both choose the same directory, e.g. even on a full disk.
func canWrite(path string) bool {
	if info, err := os.Stat(path); err == nil {
		return info.IsDir() && isWritable(path, info)
	}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		if info, err := os.Stat(p); err == nil {
			return isWritable(p, info)
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
}

// HomeDir returns the user's home directory.
//
// It checks the same environment variables as the legacy CLI, in order: {ENV_PREFIX}HOME, HOME and USERPROFILE.
// On Windows, HOME can differ from USERPROFILE, e.g. in MSYS2 or Cygwin. As in the legacy CLI, the directory must
// exist, and its real path is returned.
func (c *Config) HomeDir() (string, error) {
	for _, name := range []string{c.Application.EnvPrefix + "HOME", "HOME", "USERPROFILE"} {
		v := os.Getenv(name)
		if v == "" {
			continue
		}
		// G703: the user chooses their home directory.
		if info, err := os.Stat(v); err != nil || !info.IsDir() { //nolint:gosec
			return "", fmt.Errorf("invalid environment variable %s: %s (not a directory)", name, v)
		}
		// Resolve the path like PHP's realpath.
		if abs, err := filepath.Abs(v); err == nil {
			if resolved, err := filepath.EvalSymlinks(abs); err == nil {
				return resolved, nil
			}
		}
		return v, nil
	}
	return os.UserHomeDir()
}
