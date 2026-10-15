package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalDir(t *testing.T) {
	t.Parallel()
	repo := initGitRepo(t)
	localDir := filepath.Join(repo, ".platform", "local")
	require.NoError(t, os.MkdirAll(filepath.Join(localDir, "builds"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "project.yaml"), []byte("id: abc123\n"), 0o644))
	subdir := filepath.Join(repo, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o755))

	f := newCommandFactory(t, "", "")
	f.dir = subdir

	assertTrimmed(t, repo, f.Run("local:dir"))
	assertTrimmed(t, localDir, f.Run("local:dir", "local"))
	assertTrimmed(t, filepath.Join(localDir, "builds"), f.Run("local:dir", "builds"))

	_, stderr, err := f.RunCombinedOutput("local:dir", "shared")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Directory not found: "+filepath.Join(localDir, "shared"))

	_, stderr, err = f.RunCombinedOutput("local:dir", "other")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Unknown subdirectory: other")

	f.dir = t.TempDir()
	_, stderr, err = f.RunCombinedOutput("local:dir")
	assertExitCode(t, 2, err)
	assert.Contains(t, stderr, "Project root not found.")
}
