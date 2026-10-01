package store

import (
	"slices"

	"github.com/godbus/dbus/v5"
)

const secretServiceName = "org.freedesktop.secrets"

// secretServiceAvailable checks whether the Secret Service D-Bus name is owned or can be activated.
func secretServiceAvailable() bool {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		return false
	}
	if err := conn.Hello(); err != nil {
		return false
	}
	var hasOwner bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, secretServiceName).Store(&hasOwner); err == nil && hasOwner {
		return true
	}
	var activatable []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return false
	}
	return slices.Contains(activatable, secretServiceName)
}
