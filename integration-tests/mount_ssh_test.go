package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

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
		"df": "cat <<'OUT'\n" + strings.ReplaceAll(`Filesystem 1-blocks Used Available Capacity Mounted on
/dev/root 8589934592 4294967296 4294967296 50% /
/dev/sdb 1073741824 268435456 805306368 25% APP_DIR/var/log
/dev/sdb 1073741824 268435456 805306368 25% APP_DIR/web/uploads
tmpfs 536870912 0 536870912 0% APP_DIR/tmp
`, "APP_DIR", appDir) + "OUT\n",
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

// TestMountTransferTargets checks the options that select the files, container and instance of a transfer.
func TestMountTransferTargets(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
	env := sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Mounts: map[string]mockapi.Mount{
				"files": {Source: "storage", SourcePath: "files"},
			}},
		},
		Workers: map[string]mockapi.Worker{
			"app--queue": {App: mockapi.App{Name: "app--queue", Type: "golang:1.23", Size: "S", Mounts: map[string]mockapi.Mount{
				"files": {Source: "storage", SourcePath: "queue-files"},
			}}},
		},
	})
	env.Links["pf:ssh:app:0"] = mockapi.HALLink{HREF: "ssh://app-0@ssh.cli-tests.example.com"}
	env.Links["pf:ssh:app:1"] = mockapi.HALLink{HREF: "ssh://app-1@ssh.cli-tests.example.com"}
	p := setUpProject(t, env)

	// Each SSH user (container or instance) has its own directory, with a file naming it.
	dirs := map[string]string{}
	handlers := map[string]mockssh.CommandHandler{}
	users := []string{layoutProjectID + "-main--app", layoutProjectID + "-main--app--queue", "app-0", "app-1"}
	for _, user := range users {
		dirs[user] = t.TempDir()
		writeFiles(t, dirs[user], map[string]string{"files/" + user + ".txt": user})
		handlers[user] = mockssh.ExecHandler(dirs[user], os.Environ())
	}
	p.serveSSH(t, func(conn ssh.ConnMetadata, command string, cio mockssh.CommandIO) int {
		return handlers[conn.User()](conn, command, cio)
	})
	f := p.f

	cases := []struct {
		name string
		args []string
		user string
	}{
		{name: "app", user: layoutProjectID + "-main--app"},
		{name: "worker", args: []string{"--worker", "queue"}, user: layoutProjectID + "-main--app--queue"},
		{name: "instance", args: []string{"--instance", "1"}, user: "app-1"},
	}
	for _, c := range cases {
		target := t.TempDir()
		args := append([]string{"mount:download", "-p", layoutProjectID, "-e", "main", "-m", "files",
			"--target", target, "--yes"}, c.args...)
		_, stdErr, err := f.RunCombinedOutput(args...)
		require.NoError(t, err, c.name)
		assert.Contains(t, stdErr, "Downloading files from the remote mount files", c.name)
		assert.Equal(t, c.user, readFile(t, filepath.Join(target, c.user+".txt")), c.name)
		entries, err := os.ReadDir(target)
		require.NoError(t, err)
		assert.Len(t, entries, 1, c.name)

		source := t.TempDir()
		writeFiles(t, source, map[string]string{"up.txt": c.name})
		args = append([]string{"mount:upload", "-p", layoutProjectID, "-e", "main", "-m", "files",
			"--source", source, "--yes"}, c.args...)
		f.Run(args...)
		assert.Equal(t, c.name, readFile(t, filepath.Join(dirs[c.user], "files", "up.txt")), c.name)
	}

	_, stdErr, err := f.RunCombinedOutput("mount:download", "-p", layoutProjectID, "-e", "main", "-m", "files",
		"--target", t.TempDir(), "--instance", "2", "--yes")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Available instances: 0, 1")

	// Include rules take precedence over exclude rules.
	user := layoutProjectID + "-main--app"
	writeFiles(t, dirs[user], map[string]string{"files/skip.log": "skip", "files/keep.log": "keep"})
	target := t.TempDir()
	f.Run("mount:download", "-p", layoutProjectID, "-e", "main", "-m", "files", "--target", target,
		"--exclude", "*.log", "--include", "keep.log", "--yes")
	assert.FileExists(t, filepath.Join(target, "keep.log"))
	assert.FileExists(t, filepath.Join(target, user+".txt"))
	assert.NoFileExists(t, filepath.Join(target, "skip.log"))

	source := t.TempDir()
	writeFiles(t, source, map[string]string{"a.tmp": "a", "b.tmp": "b"})
	f.Run("mount:upload", "-p", layoutProjectID, "-e", "main", "-m", "files", "--source", source,
		"--exclude", "*.tmp", "--include", "b.tmp", "--yes")
	assert.NoFileExists(t, filepath.Join(dirs[user], "files", "a.tmp"))
	assert.FileExists(t, filepath.Join(dirs[user], "files", "b.tmp"))
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
