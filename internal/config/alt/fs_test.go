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
		t.Setenv("XDG_CONFIG_HOME", tempDir)

		result, err := FindConfigDir()
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(tempDir, subDir), result)
	})

	t.Run("HOME fallback", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", tempDir)

		result, err := FindConfigDir()
		assert.NoError(t, err)
		// On platforms where os.UserConfigDir resolves to an existing directory under the test
		// HOME, that directory wins.
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

	originalExecFn := executableFn
	t.Cleanup(func() { executableFn = originalExecFn })

	setExe := func(p string) { executableFn = func() (string, error) { return p, nil } }

	t.Run("home fallback when nothing on PATH", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("HOME", tempDir)
		t.Setenv("PATH", "/nonexistent/dir")
		t.Setenv("XDG_BIN_HOME", "")
		setExe(filepath.Join(tempDir, "exe"))

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(tempDir, homeSubDir, "bin"), result)
	})

	t.Run("allowlist fallback picks first writable PATH entry", func(t *testing.T) {
		tempDir := t.TempDir()
		localBin := filepath.Join(tempDir, ".local", "bin")
		require.NoError(t, os.MkdirAll(localBin, 0o755))
		t.Setenv("HOME", tempDir)
		t.Setenv("PATH", localBin)
		t.Setenv("XDG_BIN_HOME", "")
		setExe(filepath.Join(tempDir, "build", "exe"))

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
		t.Setenv("HOME", tempDir)
		t.Setenv("PATH", higherPriorityBin+string(os.PathListSeparator)+sourceBin)
		t.Setenv("XDG_BIN_HOME", "")
		setExe(filepath.Join(sourceBin, "exe"))

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, sourceBin, result)
	})

	t.Run("linuxbrew-style symlinked source dir is co-located via allowlist", func(t *testing.T) {
		tempDir := t.TempDir()
		brewBin := filepath.Join(tempDir, ".linuxbrew", "bin")
		cellarBin := filepath.Join(tempDir, ".linuxbrew", "Cellar", "upsun", "1.0.0", "bin")
		require.NoError(t, os.MkdirAll(brewBin, 0o755))
		require.NoError(t, os.MkdirAll(cellarBin, 0o755))
		cellarExe := filepath.Join(cellarBin, "exe")
		require.NoError(t, os.WriteFile(cellarExe, []byte("#!/bin/sh\n"), 0o600))
		require.NoError(t, os.Symlink(cellarExe, filepath.Join(brewBin, "exe")))

		higherPriorityBin := filepath.Join(tempDir, ".local", "bin")
		require.NoError(t, os.MkdirAll(higherPriorityBin, 0o755))
		t.Setenv("HOME", tempDir)
		t.Setenv("PATH", higherPriorityBin+string(os.PathListSeparator)+brewBin)
		// XDG_BIN_HOME pulls brewBin onto the allowlist regardless of host GOOS — the hardcoded
		// /home/linuxbrew/.linuxbrew/bin entry only matches on a real Linuxbrew install.
		t.Setenv("XDG_BIN_HOME", brewBin)
		setExe(cellarExe)

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, brewBin, result)
	})

	t.Run("nvm-style source dir is not selected", func(t *testing.T) {
		tempDir := t.TempDir()
		nvmBin := filepath.Join(tempDir, ".nvm", "versions", "node", "v20.0.0", "bin")
		localBin := filepath.Join(tempDir, ".local", "bin")
		require.NoError(t, os.MkdirAll(nvmBin, 0o755))
		require.NoError(t, os.MkdirAll(localBin, 0o755))
		t.Setenv("HOME", tempDir)
		t.Setenv("PATH", nvmBin+string(os.PathListSeparator)+localBin)
		t.Setenv("XDG_BIN_HOME", "")
		setExe(filepath.Join(nvmBin, "upsun"))

		result, err := FindBinDir()
		assert.NoError(t, err)
		assert.Equal(t, localBin, result)
	})

	t.Run("XDG_BIN_HOME is honored", func(t *testing.T) {
		tempDir := t.TempDir()
		xdgBin := filepath.Join(tempDir, "xdg-bin")
		require.NoError(t, os.MkdirAll(xdgBin, 0o755))
		t.Setenv("HOME", tempDir)
		t.Setenv("XDG_BIN_HOME", xdgBin)
		t.Setenv("PATH", xdgBin)
		setExe(filepath.Join(tempDir, "exe"))

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

		t.Setenv("HOME", tempDir)
		t.Setenv("PATH", readOnlyBin+string(os.PathListSeparator)+writableBin)
		t.Setenv("XDG_BIN_HOME", "")
		setExe(filepath.Join(tempDir, "exe"))

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
