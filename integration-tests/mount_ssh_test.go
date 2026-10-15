package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

func mountsDeployment() *mockapi.Deployment {
	return &mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Mounts: map[string]mockapi.Mount{
				"/var/log":    {Source: "storage", SourcePath: "logs"},
				"web/uploads": {Source: "storage", SourcePath: "uploads"},
				"tmp":         {Source: "tmp", SourcePath: "tmp"},
			}},
		},
	}
}

func TestMountSize(t *testing.T) {
	t.Parallel()
	appDir := t.TempDir()
	p := setUpProject(t, sshEnv(mountsDeployment()))
	// The df output has two mounts on one disk and one on another. Each du output is the path's length in KiB.
	env := remoteEnv(t, map[string]string{
		"df": `cat <<OUT
Filesystem 1-blocks Used Available Capacity Mounted on
/dev/root 8589934592 4294967296 4294967296 50% /
/dev/sdb 1073741824 268435456 805306368 25% $PLATFORM_APP_DIR/var/log
/dev/sdb 1073741824 268435456 805306368 25% $PLATFORM_APP_DIR/web/uploads
tmpfs 536870912 0 536870912 0% $PLATFORM_APP_DIR/tmp
OUT`,
		"du": `for path; do :; done; printf '%d\t%s\n' $((${#path} * 1024)) "$path"`,
	}, nil)
	p.serveSSH(t, mockssh.ExecHandler(appDir, append(env, "PLATFORM_APP_DIR="+appDir)))
	f := p.f

	output, stdErr, err := f.RunCombinedOutput("mount:size", "-p", layoutProjectID, "-e", "main", "--bytes",
		"--format", "csv")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Checking disk usage for all mounts")
	assert.Equal(t, `Mount(s),Size(s),Disk,Used,Available,% Used
"var/log
web/uploads","7168
11264",1073741824,268435456,805306368,25%
tmp,3072,536870912,0,536870912,0%
`, output)

	output = f.Run("mount:size", "-p", layoutProjectID, "-e", "main", "--columns", "mounts,sizes,max")
	assert.Contains(t, output, "| var/log     | 7 KiB   | 1.0 GiB   |\n| web/uploads | 11 KiB  |           |\n")
	assert.Contains(t, output, "| tmp         | 3 KiB   | 512.0 MiB |\n")
}

func TestMountUpload(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
	remoteDir := t.TempDir()
	writeFiles(t, remoteDir, map[string]string{"web/uploads/old.txt": "old"})

	p := setUpProject(t, sshEnv(mountsDeployment()))
	p.serveSSH(t, mockssh.ExecHandler(remoteDir, os.Environ()))
	f := p.f

	source := t.TempDir()
	writeFiles(t, source, map[string]string{"a.txt": "a", "sub/b.txt": "b", "skip.log": "skip"})

	_, stdErr, err := f.RunCombinedOutput("mount:upload", "-p", layoutProjectID, "-e", "main", "-m", "web/uploads",
		"--source", source, "--exclude", "*.log", "--yes")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "to the remote mount web/uploads")
	uploads := filepath.Join(remoteDir, "web", "uploads")
	assert.Equal(t, "a", readFile(t, filepath.Join(uploads, "a.txt")))
	assert.Equal(t, "b", readFile(t, filepath.Join(uploads, "sub", "b.txt")))
	assert.NoFileExists(t, filepath.Join(uploads, "skip.log"))
	assert.FileExists(t, filepath.Join(uploads, "old.txt"))

	f.Run("mount:upload", "-p", layoutProjectID, "-e", "main", "-m", "web/uploads", "--source", source,
		"--delete", "--yes")
	assert.NoFileExists(t, filepath.Join(uploads, "old.txt"))
	assert.FileExists(t, filepath.Join(uploads, "skip.log"))

	// Download it back, excluding a file and deleting extraneous local files.
	target := t.TempDir()
	writeFiles(t, target, map[string]string{"extra.txt": "extra"})
	f.Run("mount:download", "-p", layoutProjectID, "-e", "main", "-m", "web/uploads", "--target", target,
		"--exclude", "a.txt", "--delete", "--yes")
	assert.Equal(t, "b", readFile(t, filepath.Join(target, "sub", "b.txt")))
	assert.NoFileExists(t, filepath.Join(target, "a.txt"))
	assert.NoFileExists(t, filepath.Join(target, "extra.txt"))
}

// writeFiles writes files, by relative path, in a directory.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}
