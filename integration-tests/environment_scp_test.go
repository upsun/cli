package tests

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

func TestEnvironmentScp(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("scp"); err != nil {
		t.Skip("scp is not installed")
	}
	remoteDir := t.TempDir()
	writeFiles(t, remoteDir, map[string]string{"remote.txt": "remote", "logs/a.log": "a", "logs/b.log": "b"})

	p := setUpProject(t, sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Mounts: map[string]mockapi.Mount{}},
		},
	}))
	// Older scp clients run "scp -t" or "scp -f" instead of using SFTP.
	sshServer := p.serveSSH(t, mockssh.ExecHandler(remoteDir, nil))
	sshServer.SFTPDir = remoteDir
	f := p.f
	f.dir = t.TempDir()
	writeFiles(t, f.dir, map[string]string{"local.txt": "local"})
	args := []string{"scp", "-p", layoutProjectID, "-e", "main"}

	f.Run(append(args, "--", "local.txt", "remote:uploaded.txt")...)
	assert.Equal(t, "local", readFile(t, filepath.Join(remoteDir, "uploaded.txt")))

	f.Run(append(args, "--", "remote:remote.txt", "downloaded.txt")...)
	assert.Equal(t, "remote", readFile(t, filepath.Join(f.dir, "downloaded.txt")))

	f.Run(append(args, "-r", "--", "remote:logs", "logs")...)
	assert.Equal(t, "a", readFile(t, filepath.Join(f.dir, "logs", "a.log")))
	assert.Equal(t, "b", readFile(t, filepath.Join(f.dir, "logs", "b.log")))

	_, stdErr, err := f.RunCombinedOutput(append(args, "--", "remote:missing.txt", ".")...)
	require.Error(t, err)
	assert.Contains(t, stdErr, "missing.txt")

	_, stdErr, err = f.RunCombinedOutput(append(args, "--", "local.txt", "other.txt")...)
	require.Error(t, err)
	assert.Contains(t, stdErr, `At least one argument needs to contain the "remote:" prefix`)
}
