package alt

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindConfigDir(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("XDG_CONFIG_HOME exists", func(t *testing.T) {
		if runtime.GOOS == "plan9" {
			t.Skip()
		}
		require.NoError(t, os.Setenv("XDG_CONFIG_HOME", tempDir))
		t.Cleanup(func() { _ = os.Unsetenv("XDG_CONFIG_HOME") })

		result, err := FindConfigDir()
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(tempDir, subDir), result)
	})

	t.Run("HOME fallback", func(t *testing.T) {
		_ = os.Unsetenv("XDG_CONFIG_HOME")
		require.NoError(t, os.Setenv("HOME", tempDir))
		t.Cleanup(func() { _ = os.Unsetenv("HOME") })

		result, err := FindConfigDir()
		assert.NoError(t, err)
		// On platforms where os.UserConfigDir resolves to an existing directory under the test
		// HOME, that directory wins. Only assert the home fallback when it doesn't.
		ucd, ucdErr := os.UserConfigDir()
		isDir, _ := isExistingDirectory(ucd)
		if ucdErr != nil || !isDir {
			assert.Equal(t, filepath.Join(tempDir, homeSubDir), result)
		}
	})
}

func TestFindBinDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path setup in this test is unix-style")
	}

	originalPath := os.Getenv("PATH")
	originalHome := os.Getenv("HOME")
	originalXDGBin := os.Getenv("XDG_BIN_HOME")
	originalExecFn := executableFn
	t.Cleanup(func() {
		_ = os.Setenv("PATH", originalPath)
		_ = os.Setenv("HOME", originalHome)
		_ = os.Setenv("XDG_BIN_HOME", originalXDGBin)
		executableFn = originalExecFn
	})

	t.Run("home fallback when nothing on PATH", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, os.Setenv("HOME", tempDir))
		require.NoError(t, os.Setenv("PATH", "/nonexistent/dir"))
		_ = os.Unsetenv("XDG_BIN_HOME")
		executableFn = func() (string, error) { return filepath.Join(tempDir, "exe"), nil }

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(tempDir, homeSubDir, "bin"), result)
	})

	t.Run("allowlist fallback picks first writable PATH entry", func(t *testing.T) {
		tempDir := t.TempDir()
		localBin := filepath.Join(tempDir, ".local", "bin")
		require.NoError(t, os.MkdirAll(localBin, 0o755))
		require.NoError(t, os.Setenv("HOME", tempDir))
		require.NoError(t, os.Setenv("PATH", localBin))
		_ = os.Unsetenv("XDG_BIN_HOME")
		// Executable lives outside the allowlist; should not be selected.
		executableFn = func() (string, error) {
			return filepath.Join(tempDir, "build", "exe"), nil
		}

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, localBin, result)
	})

	t.Run("source-dir match on allowlist co-locates", func(t *testing.T) {
		tempDir := t.TempDir()
		sourceBin := filepath.Join(tempDir, "bin")
		higherPriorityBin := filepath.Join(tempDir, ".local", "bin")
		require.NoError(t, os.MkdirAll(sourceBin, 0o755))
		require.NoError(t, os.MkdirAll(higherPriorityBin, 0o755))
		require.NoError(t, os.Setenv("HOME", tempDir))
		require.NoError(t, os.Setenv("PATH", higherPriorityBin+string(os.PathListSeparator)+sourceBin))
		_ = os.Unsetenv("XDG_BIN_HOME")
		// Executable lives in the lower-priority allowlist entry; we must pick that one over the
		// higher-priority one.
		executableFn = func() (string, error) { return filepath.Join(sourceBin, "exe"), nil }

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, sourceBin, result)
	})

	t.Run("nvm-style source dir is not selected", func(t *testing.T) {
		tempDir := t.TempDir()
		nvmBin := filepath.Join(tempDir, ".nvm", "versions", "node", "v20.0.0", "bin")
		localBin := filepath.Join(tempDir, ".local", "bin")
		require.NoError(t, os.MkdirAll(nvmBin, 0o755))
		require.NoError(t, os.MkdirAll(localBin, 0o755))
		require.NoError(t, os.Setenv("HOME", tempDir))
		require.NoError(t, os.Setenv("PATH", nvmBin+string(os.PathListSeparator)+localBin))
		_ = os.Unsetenv("XDG_BIN_HOME")
		executableFn = func() (string, error) { return filepath.Join(nvmBin, "upsun"), nil }

		result, err := FindBinDir()
		assert.NoError(t, err)
		// Must skip the nvm dir (not on allowlist) and pick the allowlist entry.
		assert.Equal(t, localBin, result)
	})

	t.Run("XDG_BIN_HOME is honored", func(t *testing.T) {
		tempDir := t.TempDir()
		xdgBin := filepath.Join(tempDir, "xdg-bin")
		require.NoError(t, os.MkdirAll(xdgBin, 0o755))
		require.NoError(t, os.Setenv("HOME", tempDir))
		require.NoError(t, os.Setenv("XDG_BIN_HOME", xdgBin))
		require.NoError(t, os.Setenv("PATH", xdgBin))
		executableFn = func() (string, error) { return filepath.Join(tempDir, "exe"), nil }

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, xdgBin, result)
	})

	t.Run("non-writable PATH entry is skipped", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root; cannot create a non-writable directory")
		}
		tempDir := t.TempDir()
		readOnlyBin := filepath.Join(tempDir, ".local", "bin")
		writableBin := filepath.Join(tempDir, "bin")
		require.NoError(t, os.MkdirAll(readOnlyBin, 0o755))
		require.NoError(t, os.MkdirAll(writableBin, 0o755))
		require.NoError(t, os.Chmod(readOnlyBin, 0o555))
		t.Cleanup(func() { _ = os.Chmod(readOnlyBin, 0o755) })

		require.NoError(t, os.Setenv("HOME", tempDir))
		require.NoError(t, os.Setenv("PATH", readOnlyBin+string(os.PathListSeparator)+writableBin))
		_ = os.Unsetenv("XDG_BIN_HOME")
		executableFn = func() (string, error) { return filepath.Join(tempDir, "exe"), nil }

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, writableBin, result)
	})
}

func TestFSHelpers(t *testing.T) {
	tempDir := t.TempDir()

	require.NoError(t, writeFile(filepath.Join(tempDir, "test.txt"), []byte("test"), 0, 0o644))
	require.NoError(t, writeFile(filepath.Join(tempDir, "subdir", "test2.txt"), []byte("test2"), 0o755, 0o644))

	dirExists, err := isExistingDirectory(filepath.Join(tempDir, "subdir"))
	assert.NoError(t, err)
	assert.True(t, dirExists)

	dirExists, err = isExistingDirectory(filepath.Join(tempDir, "not-a-subdir"))
	assert.NoError(t, err)
	assert.False(t, dirExists)

	dirExists, err = isExistingDirectory(filepath.Join(tempDir, "test.txt"))
	assert.NoError(t, err)
	assert.False(t, dirExists)
}
