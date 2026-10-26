package commands

import (
	"errors"
	"net"
	"os"
)

// newSocketPair is not supported on Windows, where processes cannot inherit extra file descriptors.
func newSocketPair() (net.Conn, *os.File, error) {
	return nil, nil, errors.New("not supported on Windows")
}
