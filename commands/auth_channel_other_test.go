//go:build !linux && !darwin && !windows

package commands

import (
	"errors"
	"net"
)

const authChannelSupported = false

func dialAuthChannel(func(string) string) (net.Conn, error) {
	return nil, errors.ErrUnsupported
}
