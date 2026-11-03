package store

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeychainSupported_Container(t *testing.T) {
	for _, k := range []string{"SNAP_CONTEXT", "container"} {
		t.Run(k, func(t *testing.T) {
			for _, other := range []string{"SNAP_CONTEXT", "container", "DOCKER_IP"} {
				unsetenv(t, other)
			}
			t.Setenv(k, "x")
			assert.False(t, KeychainSupported())
		})
	}
}

func TestSecretServiceUnlocked(t *testing.T) {
	cases := []struct {
		name string
		// providerName is a name owned by the Secret Service's connection, if any.
		providerName string
		// otherName is a name owned by another connection, if any.
		otherName string
		locked    bool
		noService bool
		want      bool
	}{
		{name: "GNOME Keyring", providerName: "org.gnome.keyring", want: true},
		{name: "KWallet 6 (ksecretd)", providerName: "org.kde.ksecretd", want: true},
		{name: "KWallet 6 (kwalletd6)", providerName: "org.kde.kwalletd6", want: true},
		{name: "KWallet 5", providerName: "org.kde.kwalletd5", want: true},
		{name: "locked", providerName: "org.gnome.keyring", locked: true},
		{name: "KeePassXC", providerName: "org.keepassxc.KeePassXC.MainWindow"},
		{name: "unknown provider"},
		{name: "known name owned by another process", otherName: "org.gnome.keyring"},
		{name: "no Secret Service", otherName: "org.gnome.keyring", noService: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr := startSessionBus(t)
			t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
			if !c.noService {
				conn := connectBus(t, addr)
				serveSecretService(t, conn, c.locked)
				if c.providerName != "" {
					requestName(t, conn, c.providerName)
				}
			}
			if c.otherName != "" {
				requestName(t, connectBus(t, addr), c.otherName)
			}
			assert.Equal(t, c.want, secretServiceUnlocked())
		})
	}
}

// startSessionBus starts a private D-Bus daemon and returns its address.
func startSessionBus(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	// t.TempDir() includes the test name, which may have characters that are not valid in a D-Bus address.
	dir, err := os.MkdirTemp("", "dbus")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	addrArg := "--address=unix:path=" + filepath.Join(dir, "bus")
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--nopidfile", "--print-address", addrArg)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	addr, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	return strings.TrimSpace(addr)
}

func connectBus(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func requestName(t *testing.T, conn *dbus.Conn, name string) {
	t.Helper()
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	require.NoError(t, err)
	require.Equal(t, dbus.RequestNameReplyPrimaryOwner, reply)
}

// serveSecretService exports the Secret Service properties that are checked, with a default collection.
func serveSecretService(t *testing.T, conn *dbus.Conn, locked bool) {
	t.Helper()
	_, err := prop.Export(conn, "/org/freedesktop/secrets", prop.Map{
		"org.freedesktop.Secret.Service": {"Collections": {Value: []dbus.ObjectPath{defaultCollection}}},
	})
	require.NoError(t, err)
	_, err = prop.Export(conn, defaultCollection, prop.Map{
		"org.freedesktop.Secret.Collection": {"Locked": {Value: locked}},
	})
	require.NoError(t, err)
	requestName(t, conn, secretServiceName)
}

// unsetenv unsets an environment variable for the test. t.Setenv restores its value afterwards.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	_ = os.Unsetenv(key)
}
