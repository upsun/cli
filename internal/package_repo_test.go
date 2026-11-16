package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnyFileEnablesRepo(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	write("other.list", "deb http://deb.example.org stable main\n")
	write("example.list", "deb https://repo.example.com/debian stable main\n")
	write("example.list.distUpgrade", "deb https://backup.example.com/debian stable main\n")

	patterns := []string{filepath.Join(dir, "other.list"), filepath.Join(dir, "*.list")}
	assert.True(t, anyFileEnablesRepo(patterns, nil, "repo.example.com"))
	assert.False(t, anyFileEnablesRepo(patterns, nil, "other.example.com"))
	assert.False(t, anyFileEnablesRepo(patterns, nil, "backup.example.com"))
	assert.False(t, anyFileEnablesRepo(patterns[:1], nil, "repo.example.com"))
	assert.False(t, anyFileEnablesRepo(nil, nil, "repo.example.com"))
}

func TestAnyFileEnablesRepoWithOverrides(t *testing.T) {
	const repoFile = "[other]\nbaseurl=https://rpm.example.org/\n\n" +
		"[example]\nname=Example\nbaseurl=https://repo.example.com/rpm/$basearch\nenabled=1\n"
	cases := []struct {
		name string
		// Override files by path, relative to a temporary directory.
		overrides map[string]string
		want      bool
	}{
		{"no overrides", nil, true},
		{"disabled", map[string]string{"etc/99-config_manager.repo": "# Generated.\n[example]\nenabled=0\n"}, false},
		{"another repository disabled", map[string]string{"etc/99-config_manager.repo": "[other]\nenabled=0\n"}, true},
		{"disabled by a glob pattern", map[string]string{"etc/50-all.repo": "[exam*]\nenabled = false\n"}, false},
		{"another option set", map[string]string{"etc/50-x.repo": "[example]\npriority=10\n"}, true},
		{"enabled again by a later file", map[string]string{
			"etc/50-x.repo": "[example]\nenabled=0\n",
			"etc/60-x.repo": "[example]\nenabled=1\n",
		}, true},
		{"later file name in another directory", map[string]string{
			"etc/40-x.repo": "[example]\nenabled=1\n",
			"usr/50-x.repo": "[example]\nenabled=0\n",
		}, false},
		{"masked by a file of the same name", map[string]string{
			"usr/50-x.repo": "[example]\nenabled=0\n",
			"etc/50-x.repo": "[example]\npriority=10\n",
		}, true},
		{"not a repo file", map[string]string{"etc/50-x.conf": "[example]\nenabled=0\n"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range []string{"repos", "usr", "etc"} {
				require.NoError(t, os.Mkdir(filepath.Join(dir, d), 0o700))
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "repos", "example.repo"), []byte(repoFile), 0o600))
			for name, content := range c.overrides {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
			}
			patterns := []string{filepath.Join(dir, "repos", "*.repo")}
			overrides := []string{filepath.Join(dir, "usr", "*.repo"), filepath.Join(dir, "etc", "*.repo")}
			assert.Equal(t, c.want, anyFileEnablesRepo(patterns, overrides, "repo.example.com"))
		})
	}
}

func TestEnablesRepo(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"apt list", "deb [signed-by=/k.asc] https://repo.example.com/debian stable main\n", true},
		{"apt list disabled by a release upgrade", "# deb https://repo.example.com/debian stable main # disabled\n", false},
		{"deb822", "Types: deb\nURIs: https://repo.example.com/debian\nSuites: stable\n", true},
		{"deb822 disabled", "Types: deb\nURIs: https://repo.example.com/debian\nEnabled: no\n", false},
		{"yum", "[example]\nbaseurl=https://repo.example.com/rpm/$basearch\nenabled=1\ngpgcheck=1\n", true},
		{"yum disabled", "[example]\nbaseurl=https://repo.example.com/rpm/$basearch\nenabled = 0\n", false},
		{"yum disabled with off", "[example]\nbaseurl=https://repo.example.com/rpm/$basearch\nenabled=off\n", false},
		{"deb822 disabled with disable", "URIs: https://repo.example.com/debian\nEnabled: disable\n", false},
		{"deb822 disabled with without", "URIs: https://repo.example.com/debian\nEnabled: without\n", false},
		{"apk", "https://dl-cdn.example.org/alpine/main\nhttps://repo.example.com/alpine\n", true},
		{"apk commented out", "https://dl-cdn.example.org/alpine/main\n  #https://repo.example.com/alpine\n", false},
		{"another repository", "deb http://deb.example.org stable main\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, enablesRepo([]byte(c.content), "repo.example.com"))
		})
	}
}
