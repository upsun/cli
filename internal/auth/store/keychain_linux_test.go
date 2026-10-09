package store

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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

// unsetenv unsets an environment variable for the test. t.Setenv restores its value afterwards.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	_ = os.Unsetenv(key)
}
