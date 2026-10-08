package store

import (
	"os"
	"runtime"
)

// KeychainSupported reports whether the system keychain is likely to work.
//
// On Linux it requires a Secret Service on the D-Bus session bus, e.g. GNOME Keyring or KWallet, and not being in a
// snap or a container. See keychainUsableOnLinux.
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
		available, unlocked := secretServiceStatus()
		return keychainUsableOnLinux(hasDisplay(), available, unlocked)
	default:
		return false
	}
}

// keychainUsableOnLinux decides whether to use the Secret Service.
//
// The Secret Service shows any unlock prompt on the desktop's display, whatever the CLI's environment. So a locked
// keychain is only used if the CLI has a display too, as a sign that the user is at that desktop to answer the
// prompt. Over SSH it would block, and prompt on a screen the user cannot see.
func keychainUsableOnLinux(display, available, unlocked bool) bool {
	return available && (unlocked || display)
}

// hasDisplay reports whether there is an X11 or Wayland display.
func hasDisplay() bool {
	if d := os.Getenv("DISPLAY"); d != "" && d != "none" {
		return true
	}
	return os.Getenv("WAYLAND_DISPLAY") != ""
}
