//go:build linux || darwin

package commands

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/upsun/cli/internal/config"
)

// authDirPrefix is the prefix of the name of an auth channel's directory, which is followed by the ID of the process
// that listens on its socket.
const authDirPrefix = "cli-auth-"

// staleAuthDirAge is the minimum age of an auth channel's directory that may be removed by another process, so that
// one is not removed before its socket is created.
const staleAuthDirAge = time.Minute

// listenAuthChannel listens on a Unix socket in a new temporary directory, which only the current user can access. Its
// path is passed to cmd in <PREFIX>WRAPPER_AUTH_SOCKET.
//
// The directory is removed when the listener is closed. Directories that are left over, if this process is ended by a
// signal, are removed by a later one, while the legacy CLI runs.
func listenAuthChannel(cnf *config.Config, cmd *exec.Cmd) (net.Listener, error) {
	parents := authSocketParents(cnf)
	var errs []error
	for _, parent := range parents {
		dir, err := os.MkdirTemp(parent, authDirPrefix+strconv.Itoa(os.Getpid())+"-")
		if err != nil {
			errs = append(errs, err)
			continue
		}
		path := filepath.Join(dir, "s")
		l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			_ = os.RemoveAll(dir)
			errs = append(errs, err)
			continue
		}
		cmd.Env = append(cmd.Env, cnf.Application.EnvPrefix+"WRAPPER_AUTH_SOCKET="+path)
		swept := make(chan struct{})
		go func() {
			defer close(swept)
			for _, p := range parents {
				removeStaleAuthDirs(p)
			}
		}()
		return &unixAuthListener{UnixListener: l, dir: dir, swept: swept}, nil
	}
	return nil, errors.Join(errs...)
}

// authSocketParents returns the directories in which to try to create the socket's directory: the system's temporary
// directory, the CLI's own (which the legacy CLI needs anyway), then /tmp.
func authSocketParents(cnf *config.Config) []string {
	var dirs []string
	add := func(dir string) {
		// A socket's path is limited to 104 bytes on macOS, and 108 on Linux.
		if dir != "" && len(dir) <= 64 && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	add(os.TempDir())
	if dir, err := cnf.TempDir(); err == nil {
		add(dir)
	}
	add("/tmp")
	return dirs
}

type unixAuthListener struct {
	*net.UnixListener
	dir   string
	swept chan struct{} // Closed when stale directories have been removed.
}

// Close stops listening and removes the socket's directory, once stale directories have been removed.
func (l *unixAuthListener) Close() error {
	err := errors.Join(l.UnixListener.Close(), os.RemoveAll(l.dir))
	<-l.swept
	return err
}

// removeStaleAuthDirs removes the current user's auth channel directories in parent that are no longer used: their
// process does not exist, and nothing listens on their socket. The socket is checked too, as the process may be in
// another PID namespace (e.g. in a container that shares the directory).
func removeStaleAuthDirs(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range entries {
		pid, ok := authDirPID(e.Name())
		// IsDir does not follow symbolic links.
		if !ok || !e.IsDir() || processExists(pid) {
			continue
		}
		dir := filepath.Join(parent, e.Name())
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || time.Since(info.ModTime()) < staleAuthDirAge {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
			continue
		}
		if socketInUse(filepath.Join(dir, "s")) {
			continue
		}
		debugLogf("Removing a stale auth channel directory: %s", dir)
		_ = os.RemoveAll(dir)
	}
}

// authDirPID returns the process ID in the name of an auth channel directory.
func authDirPID(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, authDirPrefix)
	if !ok {
		return 0, false
	}
	s, _, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, false
	}
	pid, err := strconv.Atoi(s)
	return pid, err == nil && pid > 0
}

// processExists reports whether a process exists, including one owned by another user.
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// socketInUse reports whether a Unix socket may be in use: it is only known not to be if it does not exist, or if
// connecting to it is refused.
func socketInUse(path string) bool {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, syscall.ENOENT)
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
