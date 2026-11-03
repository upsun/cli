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

// secretServiceProviders are the D-Bus names of the Secret Service providers that are used: GNOME Keyring and
// KWallet. Others, e.g. KeePassXC, may ask the user to confirm each new process that reads a secret.
var secretServiceProviders = []string{"org.gnome.keyring", "org.kde.ksecretd", "org.kde.kwalletd6", "org.kde.kwalletd5"}

// secretServiceUnlocked checks whether a known Secret Service provider is running, and its default collection is
// unlocked, without starting a session bus or the service.
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
	if !knownSecretServiceProvider(conn) {
		return false
	}
	v, err := conn.Object(secretServiceName, keyringCollection(conn)).GetProperty(
		"org.freedesktop.Secret.Collection.Locked")
	locked, ok := v.Value().(bool)
	return err == nil && ok && !locked
}

// knownSecretServiceProvider checks whether the Secret Service is provided by a process that also owns one of the
// secretServiceProviders names.
func knownSecretServiceProvider(conn *dbus.Conn) bool {
	owner, err := nameOwner(conn, secretServiceName)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(secretServiceProviders, func(name string) bool {
		o, err := nameOwner(conn, name)
		return err == nil && o == owner
	})
}

// nameOwner returns the unique connection name that owns a D-Bus name. It returns an error if the name has no owner.
func nameOwner(conn *dbus.Conn, name string) (string, error) {
	var owner string
	err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner)
	return owner, err
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
