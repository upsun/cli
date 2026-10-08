package store

import (
	"slices"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceName = "org.freedesktop.secrets"
	defaultCollection = dbus.ObjectPath("/org/freedesktop/secrets/aliases/default")
)

// secretServiceStatus checks whether the Secret Service D-Bus name is owned or can be activated, without starting a
// session bus, and whether its default collection is unlocked. The service is not started to check that.
func secretServiceStatus() (available, unlocked bool) {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return false, false
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		return false, false
	}
	if err := conn.Hello(); err != nil {
		return false, false
	}
	var hasOwner bool
	call := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, secretServiceName)
	if err := call.Store(&hasOwner); err == nil && hasOwner {
		v, err := conn.Object(secretServiceName, defaultCollection).GetProperty("org.freedesktop.Secret.Collection.Locked")
		locked, ok := v.Value().(bool)
		return true, err == nil && ok && !locked
	}
	var activatable []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return false, false
	}
	return slices.Contains(activatable, secretServiceName), false
}
