//go:build !windows

package commands

import "net"

// dialAuthChannel connects to the Unix socket, as the legacy CLI does.
func dialAuthChannel(getenv func(string) string) (net.Conn, error) {
	return net.Dial("unix", getenv("TEST_WRAPPER_AUTH_SOCKET"))
}
