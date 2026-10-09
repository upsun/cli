package store

import (
	"os"
	"runtime"
)

// KeychainSupported reports whether the system keychain is likely to work.
//
// On Linux it requires an unlocked Secret Service collection on the D-Bus session bus, e.g. from GNOME Keyring or
// KWallet, and not being in a snap or a container. A locked keychain is not used: the Secret Service shows its unlock
// prompt on the desktop, which the user may not see, e.g. over SSH.
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
		return secretServiceUnlocked()
	default:
		return false
	}
}
