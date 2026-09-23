package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the temporary directory (used by t.TempDir and the CLI) at a
// fresh directory under the user cache directory, or INTEGRATION_TESTS_TMPDIR.
// Directories under the system temporary directory are not isolated: the CLI
// searches parent directories for a Git or project root, so a stray .git or
// .platform in e.g. /tmp would leak into tests.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	parent := os.Getenv("INTEGRATION_TESTS_TMPDIR")
	if parent == "" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		parent = filepath.Join(cacheDir, "platform-test-cli-integration")
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// Tests could otherwise read or write the enclosing repository.
	if found := findProjectMarker(parent); found != "" {
		fmt.Fprintf(os.Stderr, "Cannot isolate tests: found %s above %s\n"+
			"Set INTEGRATION_TESTS_TMPDIR to a directory outside any Git repository.\n", found, parent)
		return 1
	}
	base, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(base)
	// TMPDIR is read on Unix, TMP and TEMP on Windows.
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		if err := os.Setenv(k, base); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}

// findProjectMarker returns the first .git or .platform path found in dir or its parents.
func findProjectMarker(dir string) string {
	for {
		for _, name := range []string{".git", ".platform"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
