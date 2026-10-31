//go:build !windows

package commands

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/upsun/cli/internal/config"
)

// openAuthChannel gives cmd one end of a socket pair, as an inherited file descriptor whose number is passed in
// <PREFIX>WRAPPER_AUTH_FD.
func openAuthChannel(cnf *config.Config, cmd *exec.Cmd) (authChannel, error) {
	conn, child, err := newSocketPair()
	if err != nil {
		return nil, err
	}
	fd := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, child)
	cmd.Env = append(cmd.Env, cnf.Application.EnvPrefix+"WRAPPER_AUTH_FD="+strconv.Itoa(fd))
	return &socketPairChannel{conn: conn, child: child}, nil
}

type socketPairChannel struct {
	conn  net.Conn
	child *os.File
}

// accept returns the connection: only the child process has the other end.
func (c *socketPairChannel) accept(<-chan int) (net.Conn, error) {
	return c.conn, nil
}

func (c *socketPairChannel) Close() error {
	_ = c.child.Close()
	return c.conn.Close()
}

// newSocketPair returns a connected pair of Unix sockets: a connection for this process, and a file to pass to a
// child process.
func newSocketPair() (net.Conn, *os.File, error) {
	// The fork lock stops other goroutines starting processes that would inherit the descriptors, before they are
	// marked close-on-exec (macOS has no SOCK_CLOEXEC).
	syscall.ForkLock.RLock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fds[0])
		syscall.CloseOnExec(fds[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, nil, os.NewSyscallError("socketpair", err)
	}
	f := os.NewFile(uintptr(fds[0]), "auth-channel")
	defer f.Close()
	conn, err := net.FileConn(f)
	if err != nil {
		_ = syscall.Close(fds[1])
		return nil, nil, err
	}
	return conn, os.NewFile(uintptr(fds[1]), "auth-channel"), nil
}
