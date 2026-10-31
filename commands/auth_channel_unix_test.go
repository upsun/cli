//go:build !windows

package commands

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
