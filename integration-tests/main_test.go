package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain points TMPDIR (used by t.TempDir and the CLI) at a fresh directory
// under the user cache directory. Directories under the system temporary
// directory are not isolated: the CLI searches parent directories for a
// project root, so a stray .git or .platform in e.g. /tmp would leak into tests.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	parent := filepath.Join(cacheDir, "platform-test-cli-integration")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if found := findProjectMarker(parent); found != "" {
		fmt.Fprintf(os.Stderr, "Cannot isolate tests: found %s above the test directory %s\n", found, parent)
		return 1
	}
	base, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(base)
	if err := os.Setenv("TMPDIR", base); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
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
