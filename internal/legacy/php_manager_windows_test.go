package legacy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWindowsIniSetting checks that PHP reads a path back unchanged.
//
// PHP parses ini values as expressions, so an unquoted path is truncated at a
// character such as "~", which Windows short paths contain, and inside quotes a
// backslash escapes the next character.
func TestWindowsIniSetting(t *testing.T) {
	cacheDir := t.TempDir()
	manager := newPHPManager(cacheDir)
	require.NoError(t, manager.copy())

	readSetting := func(t *testing.T, args ...string) string {
		t.Helper()
		args = append([]string{"-n"}, args...)
		args = append(args, "-r", `echo ini_get("openssl.cafile");`)
		output, err := exec.Command(manager.binPath(), args...).CombinedOutput() //nolint:gosec
		require.NoError(t, err, "PHP exited with an error: %s", output)
		return strings.TrimSpace(string(output))
	}

	cases := []struct {
		name string
		path string
	}{
		{"a path in the cache directory", filepath.Join(cacheDir, "cacert.pem")},
		{"a short path", `C:\Users\RUNNER~1\AppData\Local\cacert.pem`},
		{"a network path", `\\server\share\cacert.pem`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.path, readSetting(t, "-d", iniSetting("openssl.cafile", c.path)))
		})
	}

	t.Run("the settings the wrapper passes", func(t *testing.T) {
		assert.Equal(t, filepath.Join(cacheDir, "cacert.pem"),
			readSetting(t, settingArgs(manager.settings())...))
	})
}
