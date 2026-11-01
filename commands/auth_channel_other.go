//go:build !linux && !darwin && !windows

package commands

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"

	"github.com/upsun/cli/internal/config"
)

// listenAuthChannel fails on systems where this process cannot check the ID of the process at the other end of a
// connection, so connections from other processes could not be refused (e.g. FreeBSD, whose peer credentials
// golang.org/x/sys does not expose with the process ID). The legacy CLI runs auth:internal instead.
func listenAuthChannel(*config.Config, *exec.Cmd) (net.Listener, error) {
	return nil, errAuthChannelUnsupported
}

var errAuthChannelUnsupported = fmt.Errorf("the auth channel is not supported on %s: %w", runtime.GOOS,
	errors.ErrUnsupported)

func peerPID(net.Conn) (int, error) {
	return 0, errors.ErrUnsupported
}
