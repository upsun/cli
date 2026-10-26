//go:build !windows

package commands

import (
	"net"
	"os"
	"syscall"
)

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
