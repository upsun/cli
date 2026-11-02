//go:build !windows

package authchannel

import (
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialChannel connects to the Unix socket, as the legacy CLI does.
func dialChannel(getenv func(string) string) (net.Conn, error) {
	return net.Dial("unix", getenv("TEST_WRAPPER_AUTH_SOCKET"))
}

// TestListen_LongTempDir checks that a socket can be created when the temporary directory's path is too
// long for a socket path.
func TestListen_LongTempDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("d", 100))
	require.NoError(t, os.Mkdir(dir, 0o700))
	t.Setenv("TMPDIR", dir)
	l, err := listen(testConfig(), &exec.Cmd{}, noLog)
	require.NoError(t, err)
	require.NoError(t, l.Close())
}

// TestListen_UnusableTempDir checks that the CLI's temporary directory is used if the system's cannot be.
func TestListen_UnusableTempDir(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	cliTemp := t.TempDir()
	t.Setenv("TEST_TMP", cliTemp)
	cmd := &exec.Cmd{}
	l, err := listen(testConfig(), cmd, noLog)
	require.NoError(t, err)
	defer l.Close()
	var socket string
	for _, e := range cmd.Env {
		if v, ok := strings.CutPrefix(e, "TEST_WRAPPER_AUTH_SOCKET="); ok {
			socket = v
		}
	}
	assert.True(t, strings.HasPrefix(socket, cliTemp+string(filepath.Separator)), "socket: %s", socket)
}

// TestRemoveStaleDirs checks that only the directories of channels that are no longer used are removed.
func TestRemoveStaleDirs(t *testing.T) {
	// A short path, for the socket.
	parent, err := os.MkdirTemp("/tmp", "cli-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(parent) })

	// The ID of a process that has exited.
	exited := exec.Command("true")
	require.NoError(t, exited.Run())
	deadPID := strconv.Itoa(exited.Process.Pid)
	old := time.Now().Add(-2 * staleDirAge)
	mkdir := func(name string, mtime time.Time) string {
		dir := filepath.Join(parent, name)
		require.NoError(t, os.Mkdir(dir, 0o700))
		require.NoError(t, os.Chtimes(dir, mtime, mtime))
		return dir
	}
	stale := mkdir(dirPrefix+deadPID+"-a", old)
	// A socket that nothing listens on.
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(stale, "s"), Net: "unix"})
	require.NoError(t, err)
	l.SetUnlinkOnClose(false)
	require.NoError(t, l.Close())
	require.NoError(t, os.Chtimes(stale, old, old))
	staleEmpty := mkdir(dirPrefix+deadPID+"-b", old)

	kept := make([]string, 0, 8)
	kept = append(kept,
		mkdir(dirPrefix+deadPID+"-recent", time.Now()),
		mkdir(dirPrefix+strconv.Itoa(os.Getpid())+"-live", old),
		mkdir(dirPrefix+"abc-x", old),
		mkdir(dirPrefix+"x", old),
		mkdir("other-"+deadPID+"-x", old),
	)
	// A socket that is listened on, e.g. by a process in another PID namespace.
	listening := mkdir(dirPrefix+deadPID+"-listening", old)
	l, err = net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(listening, "s"), Net: "unix"})
	require.NoError(t, err)
	defer l.Close()
	require.NoError(t, os.Chtimes(listening, old, old))
	kept = append(kept, listening)
	// A symbolic link is not followed.
	target := mkdir("target", old)
	require.NoError(t, os.WriteFile(filepath.Join(target, "f"), nil, 0o600))
	link := filepath.Join(parent, dirPrefix+deadPID+"-link")
	require.NoError(t, os.Symlink(target, link))
	kept = append(kept, link, filepath.Join(target, "f"))

	removeStaleDirs(parent, noLog)

	for _, p := range []string{stale, staleEmpty} {
		_, err := os.Lstat(p)
		assert.ErrorIs(t, err, fs.ErrNotExist, p)
	}
	for _, p := range kept {
		_, err := os.Lstat(p)
		assert.NoError(t, err, p)
	}
}

func TestDirPID(t *testing.T) {
	cases := []struct {
		name string
		pid  int
		ok   bool
	}{
		{"cli-auth-123-abc", 123, true},
		{"cli-auth-123-", 123, true},
		{"cli-auth-123", 0, false},
		{"cli-auth-0-abc", 0, false},
		{"cli-auth--1-abc", 0, false},
		{"cli-auth-x-abc", 0, false},
		{"other-123-abc", 0, false},
	}
	for _, c := range cases {
		pid, ok := dirPID(c.name)
		assert.Equal(t, c.ok, ok, c.name)
		if c.ok {
			assert.Equal(t, c.pid, pid, c.name)
		}
	}
}
