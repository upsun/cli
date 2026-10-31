//go:build !windows

package commands

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/upsun/cli/internal/config"
)

// listenAuthChannel listens on a Unix socket in a new temporary directory, which only the current user can access. Its
// path is passed to cmd in <PREFIX>WRAPPER_AUTH_SOCKET.
func listenAuthChannel(cnf *config.Config, cmd *exec.Cmd) (net.Listener, error) {
	// A socket's path is limited to 104 bytes on macOS, and 108 on Linux.
	tmp := os.TempDir()
	if len(tmp) > 64 {
		tmp = "/tmp"
	}
	dir, err := os.MkdirTemp(tmp, "cli-auth-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "s")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	cmd.Env = append(cmd.Env, cnf.Application.EnvPrefix+"WRAPPER_AUTH_SOCKET="+path)
	return &unixAuthListener{UnixListener: l, dir: dir}, nil
}

type unixAuthListener struct {
	*net.UnixListener
	dir string
}

func (l *unixAuthListener) Close() error {
	return errors.Join(l.UnixListener.Close(), os.RemoveAll(l.dir))
}

// peerPID returns the ID of the process at the other end of a Unix socket connection.
func peerPID(conn net.Conn) (int, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("unexpected connection type: %T", conn)
	}
	rc, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var pidErr error
	if err := rc.Control(func(fd uintptr) { pid, pidErr = socketPeerPID(int(fd)) }); err != nil {
		return 0, err
	}
	return pid, pidErr
}
