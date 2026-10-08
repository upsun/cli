package store

import (
	"slices"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceName = "org.freedesktop.secrets"
	loginCollection   = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	defaultCollection = dbus.ObjectPath("/org/freedesktop/secrets/aliases/default")
)

// secretServiceUnlocked checks whether the Secret Service's default collection is unlocked, without starting a
// session bus or the service.
func secretServiceUnlocked() bool {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
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
	call := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, secretServiceName)
	if err := call.Store(&hasOwner); err != nil || !hasOwner {
		return false
	}
	v, err := conn.Object(secretServiceName, keyringCollection(conn)).GetProperty(
		"org.freedesktop.Secret.Collection.Locked")
	locked, ok := v.Value().(bool)
	return err == nil && ok && !locked
}

// keyringCollection returns the collection that go-keyring uses: "login" if it exists, or else the default.
func keyringCollection(conn *dbus.Conn) dbus.ObjectPath {
	v, err := conn.Object(secretServiceName, "/org/freedesktop/secrets").GetProperty(
		"org.freedesktop.Secret.Service.Collections")
	if paths, ok := v.Value().([]dbus.ObjectPath); err == nil && ok && slices.Contains(paths, loginCollection) {
		return loginCollection
	}
	return defaultCollection
}
