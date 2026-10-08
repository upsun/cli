package store

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeychainUsableOnLinux(t *testing.T) {
	cases := []struct {
		name                         string
		display, available, unlocked bool
		want                         bool
	}{
		{name: "unlocked, no display (e.g. SSH)", available: true, unlocked: true, want: true},
		{name: "locked, with a display to prompt on", display: true, available: true, want: true},
		{name: "locked, no display (e.g. SSH)", available: true, want: false},
		{name: "no Secret Service", display: true, unlocked: true, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, keychainUsableOnLinux(c.display, c.available, c.unlocked))
		})
	}
}

func TestHasDisplay(t *testing.T) {
	cases := []struct {
		display, wayland string
		want             bool
	}{
		{"", "", false},
		{"none", "", false},
		{":0", "", true},
		{"", "wayland-0", true},
	}
	for _, c := range cases {
		t.Setenv("DISPLAY", c.display)
		t.Setenv("WAYLAND_DISPLAY", c.wayland)
		assert.Equal(t, c.want, hasDisplay(), "DISPLAY=%q WAYLAND_DISPLAY=%q", c.display, c.wayland)
	}
}

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

// unsetenv unsets an environment variable for the test. t.Setenv restores its value afterwards.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	_ = os.Unsetenv(key)
}
