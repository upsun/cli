package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestList(t *testing.T) {
	t.Parallel()
	f := newCommandFactory(t, "", "")

	output := f.Run("list")

	assert.NotEmpty(t, output)
	assert.Contains(t, output, "Available commands:")
	assert.Contains(t, output, "activity:list (activities, act)")
	assert.NotContains(t, output, "mount:size")

	output = f.Run("list", "--all")

	assert.NotEmpty(t, output)
	assert.Contains(t, output, "Available commands:")
	assert.Contains(t, output, "mount:size")
}

// TestListXdebugInProject checks that environment:xdebug is only listed inside a project if it has a PHP app.
func TestListXdebugInProject(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		config string
		listed bool
	}{
		{name: "php", config: "applications:\n  app:\n    type: 'php:8.4'\n", listed: true},
		{name: "nodejs", config: "applications:\n  app:\n    type: 'nodejs:22'\n", listed: false},
		{
			name:   "composable with php",
			config: "applications:\n  app:\n    type: 'composable:25.11'\n    stack:\n      runtimes: ['php@8.4']\n",
			listed: true,
		},
		{
			name:   "composable with php in a stack list",
			config: "applications:\n  app:\n    type: 'composable:25.11'\n    stack: ['php@8.4', 'yarn']\n",
			listed: true,
		},
		{
			name:   "composable without php",
			config: "applications:\n  app:\n    type: 'composable:25.11'\n    stack:\n      runtimes: ['nodejs@22']\n",
			listed: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newCommandFactory(t, "", "")
			f.dir = repoLayout{git: true, branch: "main", projectYAML: true}.build(t)
			require.NoError(t, os.WriteFile(filepath.Join(f.dir, ".platform", "config.yaml"), []byte(c.config), 0o644))

			output := f.Run("list")
			if c.listed {
				assert.Contains(t, output, "environment:xdebug")
			} else {
				assert.NotContains(t, output, "environment:xdebug")
			}
		})
	}
}
