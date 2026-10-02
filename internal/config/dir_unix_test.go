//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPrivateDir(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T, path string)
		uid      int
		wantErr  string
		wantMode os.FileMode
	}{
		{
			name:     "private directory",
			setup:    func(t *testing.T, path string) { require.NoError(t, os.Mkdir(path, 0o700)) },
			uid:      os.Geteuid(),
			wantMode: 0o700,
		},
		{
			name: "shared directory is tightened",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.Mkdir(path, 0o700))
				require.NoError(t, os.Chmod(path, 0o777))
			},
			uid:      os.Geteuid(),
			wantMode: 0o700,
		},
		{
			name:    "owned by another user",
			setup:   func(t *testing.T, path string) { require.NoError(t, os.Mkdir(path, 0o700)) },
			uid:     os.Geteuid() + 1,
			wantErr: "the directory is not owned by the current user",
		},
		{
			name: "own symlink",
			setup: func(t *testing.T, path string) {
				target := filepath.Join(t.TempDir(), "target")
				require.NoError(t, os.Mkdir(target, 0o755))
				require.NoError(t, os.Symlink(target, path))
			},
			uid:      os.Geteuid(),
			wantMode: 0o700,
		},
		{
			name: "symlink owned by another user",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.Symlink(t.TempDir(), path))
			},
			uid:     os.Geteuid() + 1,
			wantErr: "the symlink is not owned by the current user",
		},
		{
			name: "symlink to a file",
			setup: func(t *testing.T, path string) {
				target := filepath.Join(t.TempDir(), "file")
				require.NoError(t, os.WriteFile(target, nil, 0o600))
				require.NoError(t, os.Symlink(target, path))
			},
			uid:     os.Geteuid(),
			wantErr: "not a directory",
		},
		{
			name: "file",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, nil, 0o600))
			},
			uid:     os.Geteuid(),
			wantErr: "not a directory",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dir")
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
