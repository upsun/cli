package store

import (
	"os"
	"runtime"
)

// KeychainSupported reports whether the system keychain is likely to work.
//
// On Linux it requires a Secret Service on the D-Bus session bus, e.g. GNOME Keyring or KWallet, and not being in a
// snap or a container. A display is not needed: the Secret Service shows any unlock prompt itself.
func KeychainSupported() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	case "linux":
		for _, v := range []string{"SNAP_CONTEXT", "container", "DOCKER_IP"} {
			if _, ok := os.LookupEnv(v); ok {
				return false
			}
		}
		if _, err := os.Stat("/.dockerenv"); err == nil {
			return false
		}
		return secretServiceAvailable()
	default:
		return false
	}
}
