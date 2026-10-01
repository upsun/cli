package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
)

func TestHomeDir(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"prefixed var first", map[string]string{"EXAMPLE_CLI_HOME": "/a", "HOME": "/b", "USERPROFILE": "/c"}, "/a"},
		{"then HOME", map[string]string{"EXAMPLE_CLI_HOME": "", "HOME": "/b", "USERPROFILE": "/c"}, "/b"},
		{"then USERPROFILE", map[string]string{"EXAMPLE_CLI_HOME": "", "HOME": "", "USERPROFILE": "/c"}, "/c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cnf, err := config.FromYAML([]byte(validConfig))
			require.NoError(t, err)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			home, err := cnf.HomeDir()
			require.NoError(t, err)
			assert.Equal(t, c.want, home)
		})
	}
}

// TestWritableUserDir_TempFallback checks the cases where the legacy CLI uses a temporary directory instead.
func TestWritableUserDir_TempFallback(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, home string)
	}{
		{
			name: "read-only home",
			setup: func(t *testing.T, home string) {
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("needs Unix permissions")
				}
				require.NoError(t, os.Chmod(home, 0o500))
				t.Cleanup(func() { _ = os.Chmod(home, 0o700) })
			},
		},
		{
			name: "a file in place of the directory",
			setup: func(t *testing.T, home string) {
				require.NoError(t, os.WriteFile(filepath.Join(home, ".example-cli"), nil, 0o600))
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cnf, err := config.FromYAML([]byte(validConfig))
			require.NoError(t, err)
			home := t.TempDir()
			c.setup(t, home)
			tmp := t.TempDir()
			t.Setenv("EXAMPLE_CLI_HOME", home)
			t.Setenv("TMPDIR", tmp)

			dir, err := cnf.WritableUserDir()
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(tmp, "example-cli-tmp"), dir)
		})
	}
}
