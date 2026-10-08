package store

import "github.com/godbus/dbus/v5"

const (
	secretServiceName = "org.freedesktop.secrets"
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
	v, err := conn.Object(secretServiceName, defaultCollection).GetProperty("org.freedesktop.Secret.Collection.Locked")
	locked, ok := v.Value().(bool)
	return err == nil && ok && !locked
}
