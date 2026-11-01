//go:build linux || darwin

package commands

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

const authChannelSupported = true

// dialAuthChannel connects to the Unix socket, as the legacy CLI does.
func dialAuthChannel(getenv func(string) string) (net.Conn, error) {
	return net.Dial("unix", getenv("TEST_WRAPPER_AUTH_SOCKET"))
}

// TestListenAuthChannel_LongTempDir checks that a socket can be created when the temporary directory's path is too
// long for a socket path.
func TestListenAuthChannel_LongTempDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("d", 100))
	require.NoError(t, os.Mkdir(dir, 0o700))
	t.Setenv("TMPDIR", dir)
	l, err := listenAuthChannel(testConfig(), &exec.Cmd{})
	require.NoError(t, err)
	require.NoError(t, l.Close())
}

// TestRemoveStaleAuthDirs checks that only the directories of channels that are no longer used are removed.
func TestRemoveStaleAuthDirs(t *testing.T) {
	// A short path, for the socket.
	parent, err := os.MkdirTemp("/tmp", "cli-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(parent) })

	// The ID of a process that has exited.
	exited := exec.Command("true")
	require.NoError(t, exited.Run())
	deadPID := strconv.Itoa(exited.Process.Pid)
	old := time.Now().Add(-2 * staleAuthDirAge)
	mkdir := func(name string, mtime time.Time) string {
		dir := filepath.Join(parent, name)
		require.NoError(t, os.Mkdir(dir, 0o700))
		require.NoError(t, os.Chtimes(dir, mtime, mtime))
		return dir
	}
	stale := mkdir(authDirPrefix+deadPID+"-a", old)
	// A socket that nothing listens on.
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(stale, "s"), Net: "unix"})
	require.NoError(t, err)
	l.SetUnlinkOnClose(false)
	require.NoError(t, l.Close())
	require.NoError(t, os.Chtimes(stale, old, old))
	staleEmpty := mkdir(authDirPrefix+deadPID+"-b", old)

	kept := make([]string, 0, 8)
	kept = append(kept,
		mkdir(authDirPrefix+deadPID+"-recent", time.Now()),
		mkdir(authDirPrefix+strconv.Itoa(os.Getpid())+"-live", old),
		mkdir(authDirPrefix+"abc-x", old),
		mkdir(authDirPrefix+"x", old),
		mkdir("other-"+deadPID+"-x", old),
	)
	// A socket that is listened on, e.g. by a process in another PID namespace.
	listening := mkdir(authDirPrefix+deadPID+"-listening", old)
	l, err = net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(listening, "s"), Net: "unix"})
	require.NoError(t, err)
	defer l.Close()
	require.NoError(t, os.Chtimes(listening, old, old))
	kept = append(kept, listening)
	// A symbolic link is not followed.
	target := mkdir("target", old)
	require.NoError(t, os.WriteFile(filepath.Join(target, "f"), nil, 0o600))
	link := filepath.Join(parent, authDirPrefix+deadPID+"-link")
	require.NoError(t, os.Symlink(target, link))
	kept = append(kept, link, filepath.Join(target, "f"))

	removeStaleAuthDirs(parent)

	for _, p := range []string{stale, staleEmpty} {
		_, err := os.Lstat(p)
		assert.ErrorIs(t, err, fs.ErrNotExist, p)
	}
	for _, p := range kept {
		_, err := os.Lstat(p)
		assert.NoError(t, err, p)
	}
}

func TestAuthDirPID(t *testing.T) {
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
		pid, ok := authDirPID(c.name)
		assert.Equal(t, c.ok, ok, c.name)
		if c.ok {
			assert.Equal(t, c.pid, pid, c.name)
		}
	}
}
