package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectShell(t *testing.T) {
	cases := []struct {
		shell string
		want  string
	}{
		{"/bin/bash", "bash"},
		{"/usr/local/bin/zsh", "zsh"},
		{"/usr/bin/fish", ""},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			t.Setenv("SHELL", c.shell)
			assert.Equal(t, c.want, detectShell())
		})
	}
}

func TestDefaultCompletionPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("non-root user paths only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	cases := []struct {
		shell string
		want  string
	}{
		{"bash", filepath.Join(home, ".local", "share", "bash-completion", "completions", "upsun")},
		{"zsh", filepath.Join(home, ".zsh", "completions", "_upsun")},
	}
	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			got, err := defaultCompletionPath("upsun", c.shell)
			assert.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	t.Run("xdg override", func(t *testing.T) {
		xdg := filepath.Join(home, "xdg")
		t.Setenv("XDG_DATA_HOME", xdg)
		got, err := defaultCompletionPath("upsun", "bash")
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(xdg, "bash-completion", "completions", "upsun"), got)
	})

	t.Run("unsupported shell", func(t *testing.T) {
		_, err := defaultCompletionPath("upsun", "fish")
		assert.Error(t, err)
	})
}
