package commands

import (
	"crypto/rand"
	"fmt"
	"net"
	"os/exec"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/upsun/cli/internal/config"
)

// listenAuthChannel listens on a new named pipe, whose name is passed to cmd in <PREFIX>WRAPPER_AUTH_PIPE.
func listenAuthChannel(cnf *config.Config, cmd *exec.Cmd) (net.Listener, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("could not get the current user: %w", err)
	}
	// Not named after the slug, which may contain characters that the legacy CLI does not accept in the name.
	name := "cli-auth-" + rand.Text()
	sddl := pipeSDDL(user.User.Sid, token.IsElevated())
	l, err := winio.ListenPipe(`\\.\pipe\`+name, &winio.PipeConfig{SecurityDescriptor: sddl})
	if err != nil {
		return nil, err
	}
	cmd.Env = append(cmd.Env, cnf.Application.EnvPrefix+"WRAPPER_AUTH_PIPE="+name)
	return l, nil
}

// pipeSDDL gives the user full access to the pipe, and no one else: the default DACL gives Everyone read access.
// go-winio also rejects remote clients, and fails if a pipe with the name exists.
//
// If elevated, the pipe gets a high integrity label: without one, the user's processes that are not elevated could also
// open it or create instances of it.
func pipeSDDL(sid *windows.SID, elevated bool) string {
	sddl := "O:" + sid.String() + "D:P(A;;GA;;;" + sid.String() + ")"
	if elevated {
		sddl += "S:(ML;;NW;;;HI)"
	}
	return sddl
}

// peerPID returns the ID of the process at the other end of a named pipe connection.
func peerPID(conn net.Conn) (int, error) {
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
