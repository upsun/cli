package store

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeychainSupported_Linux(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "no display or GNOME", env: map[string]string{"DISPLAY": "", "XDG_CURRENT_DESKTOP": "KDE"}, want: true},
		{name: "container", env: map[string]string{"container": "podman"}, want: false},
		{name: "snap", env: map[string]string{"SNAP_CONTEXT": "x"}, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []string{"SNAP_CONTEXT", "container", "DOCKER_IP"} {
				if _, ok := c.env[k]; !ok {
					unsetenv(t, k)
				}
			}
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			// The result depends on this system's Secret Service, unless the environment rules it out.
			assert.Equal(t, c.want && secretServiceAvailable(), KeychainSupported())
		})
	}
}

// unsetenv unsets an environment variable for the test. t.Setenv restores its value afterwards.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	_ = os.Unsetenv(key)
}
