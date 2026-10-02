package store

import (
	"os"
	"runtime"
	"strings"
)

// KeychainSupported reports whether the system keychain is likely to work.
//
// On Linux this keeps the legacy CLI's conditions: a display, a GNOME session, not in a snap or a container, and a
// Secret Service on D-Bus.
func KeychainSupported() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	case "linux":
		if d := os.Getenv("DISPLAY"); d == "" || d == "none" {
			return false
		}
		if !strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "GNOME") {
			return false
		}
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
