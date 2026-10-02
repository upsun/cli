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
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Symlink(a, link))
	}
	resolved := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		require.NoError(t, err)
		return r
	}
	cases := []struct {
		name    string
		env     map[string]string
		want    string
		wantErr string
	}{
		{name: "prefixed var first", env: map[string]string{"EXAMPLE_CLI_HOME": a, "HOME": b, "USERPROFILE": c}, want: a},
		{name: "then HOME", env: map[string]string{"EXAMPLE_CLI_HOME": "", "HOME": b, "USERPROFILE": c}, want: b},
		{name: "then USERPROFILE", env: map[string]string{"EXAMPLE_CLI_HOME": "", "HOME": "", "USERPROFILE": c}, want: c},
		{name: "symlink is resolved", env: map[string]string{"EXAMPLE_CLI_HOME": link}, want: a},
		{
			name:    "not a directory",
			env:     map[string]string{"EXAMPLE_CLI_HOME": filepath.Join(a, "missing")},
			wantErr: "invalid environment variable EXAMPLE_CLI_HOME",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.env["EXAMPLE_CLI_HOME"] == link && runtime.GOOS == "windows" {
				t.Skip("symlinks need privileges on Windows")
			}
			cnf, err := config.FromYAML([]byte(validConfig))
			require.NoError(t, err)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			home, err := cnf.HomeDir()
			if c.wantErr != "" {
				assert.ErrorContains(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, resolved(c.want), home)
		})
	}
}

func TestHomeDir_Relative(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cnf, err := config.FromYAML([]byte(validConfig))
	require.NoError(t, err)
	t.Setenv("EXAMPLE_CLI_HOME", ".")
	home, err := cnf.HomeDir()
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, want, home)
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
			t.Setenv("TMPDIR", tmp) // Unix
			t.Setenv("TMP", tmp)    // Windows

			dir, err := cnf.WritableUserDir()
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(tmp, "example-cli-tmp"), dir)
		})
	}
}
