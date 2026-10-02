//go:build unix

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPrivateDir(t *testing.T) {
	other := os.Geteuid() + 1
	cases := []struct {
		name     string
		shared   bool // Whether the parent directory is world-writable, like /tmp.
		setup    func(t *testing.T, path string)
		uid      int
		wantErr  string
		wantMode os.FileMode
	}{
		{
			name:     "own directory in a shared parent",
			shared:   true,
			setup:    mkdir(0o700),
			uid:      os.Geteuid(),
			wantMode: 0o700,
		},
		{
			name:     "open directory in a shared parent is tightened",
			shared:   true,
			setup:    mkdir(0o777),
			uid:      os.Geteuid(),
			wantMode: 0o700,
		},
		{
			name:    "another user's directory in a shared parent",
			shared:  true,
			setup:   mkdir(0o700),
			uid:     other,
			wantErr: "not owned by the current user",
		},
		{
			name:    "another user's symlink in a shared parent",
			shared:  true,
			setup:   func(t *testing.T, path string) { require.NoError(t, os.Symlink(t.TempDir(), path)) },
			uid:     other,
			wantErr: "not owned by the current user",
		},
		{
			name: "symlink to another user's directory in a shared parent",
			setup: func(t *testing.T, path string) {
				target := filepath.Join(sharedDir(t), "target")
				require.NoError(t, os.Mkdir(target, 0o700))
				require.NoError(t, os.Symlink(target, path))
			},
			uid:     other,
			wantErr: "not owned by the current user",
		},
		{
			// E.g. sudo -E, or an arbitrary UID in a container.
			name:     "another user's directory in a private parent",
			setup:    mkdir(0o755),
			uid:      other,
			wantMode: 0o755,
		},
		{
			name:    "file",
			shared:  true,
			setup:   func(t *testing.T, path string) { require.NoError(t, os.WriteFile(path, nil, 0o600)) },
			uid:     os.Geteuid(),
			wantErr: "not a directory",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parent := t.TempDir()
			require.NoError(t, os.Chmod(parent, 0o755))
			if c.shared {
				parent = sharedDir(t)
			}
			path := filepath.Join(parent, "dir")
			c.setup(t, path)
			err := checkPrivateDir(path, c.uid)
			if c.wantErr != "" {
				assert.ErrorContains(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, c.wantMode, info.Mode().Perm())
		})
	}
}

func mkdir(mode os.FileMode) func(t *testing.T, path string) {
	return func(t *testing.T, path string) {
		require.NoError(t, os.Mkdir(path, 0o700))
		require.NoError(t, os.Chmod(path, mode))
	}
}

// sharedDir returns a directory that others can write to, like /tmp.
func sharedDir(t *testing.T) string {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o777|os.ModeSticky))
	return dir
}
