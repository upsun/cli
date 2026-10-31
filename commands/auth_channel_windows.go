package commands

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sync"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/upsun/cli/internal/config"
)

// openAuthChannel listens on a new named pipe, whose name is passed to cmd in <PREFIX>WRAPPER_AUTH_PIPE, as processes
// cannot inherit extra file descriptors on Windows.
func openAuthChannel(cnf *config.Config, cmd *exec.Cmd) (authChannel, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("could not get the current user: %w", err)
	}
	// Not named after the slug, which may contain characters that the legacy CLI does not accept in the name.
	name := "cli-auth-" + rand.Text()
	l, err := winio.ListenPipe(`\\.\pipe\`+name, &winio.PipeConfig{SecurityDescriptor: pipeSDDL(user.User.Sid)})
	if err != nil {
		return nil, err
	}
	cmd.Env = append(cmd.Env, cnf.Application.EnvPrefix+"WRAPPER_AUTH_PIPE="+name)
	return &pipeChannel{listener: l, closed: make(chan struct{})}, nil
}

// pipeSDDL gives the user full access to the pipe, and no one else: the default DACL gives Everyone read access.
// go-winio also rejects remote clients, and fails if a pipe with the name exists.
func pipeSDDL(sid *windows.SID) string {
	return "O:" + sid.String() + "D:P(A;;GA;;;" + sid.String() + ")"
}

type pipeChannel struct {
	listener net.Listener

	mu     sync.Mutex
	closed chan struct{}
	conn   net.Conn
}

// accept returns the first connection from the child process, and stops listening. Connections from other processes,
// such as those that the child starts, which inherit its environment, are closed.
func (c *pipeChannel) accept(pid <-chan int) (net.Conn, error) {
	defer c.listener.Close()
	childPID := 0
	for {
		conn, err := c.listener.Accept()
		if err != nil {
			return nil, err
		}
		// The child may connect before this process has its PID.
		if childPID == 0 {
			select {
			case childPID = <-pid:
			case <-c.closed:
				_ = conn.Close()
				return nil, net.ErrClosed
			}
		}
		clientPID, err := pipeClientPID(conn)
		if err == nil && clientPID == childPID {
			if err := c.setConn(conn); err != nil {
				return nil, err
			}
			return conn, nil
		}
		debugLogf("Auth channel: refused a connection from process %d (%v)", clientPID, err)
		_ = conn.Close()
	}
}

// setConn keeps the connection, so that Close closes it.
func (c *pipeChannel) setConn(conn net.Conn) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
		_ = conn.Close()
		return net.ErrClosed
	default:
	}
	c.conn = conn
	return nil
}

func (c *pipeChannel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.closed)
	err := c.listener.Close()
	if c.conn != nil {
		err = errors.Join(err, c.conn.Close())
	}
	return err
}

// pipeClientPID returns the ID of the process at the other end of a named pipe connection.
func pipeClientPID(conn net.Conn) (int, error) {
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, fmt.Errorf("unexpected connection type: %T", conn)
	}
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(windows.Handle(f.Fd()), &pid); err != nil {
		return 0, err
	}
	return int(pid), nil
}
