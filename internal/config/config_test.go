package config_test

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
)

//go:embed test-data/valid-config.yaml
var validConfig string

func TestFromYAML(t *testing.T) {
	t.Run("missing_values", func(t *testing.T) {
		_, err := config.FromYAML([]byte(`application: {name: Test CLI}`))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), `Error:Field validation for 'EnvPrefix' failed on the 'required' tag`)
	})

	t.Run("complete", func(t *testing.T) {
		cnf, err := config.FromYAML([]byte(validConfig))
		assert.NoError(t, err)

		tempDir := t.TempDir()
		require.NoError(t, os.Setenv(cnf.Application.EnvPrefix+"HOME", tempDir))
		require.NoError(t, os.Setenv(cnf.Application.EnvPrefix+"TMP", filepath.Join(tempDir, "tmp")))
		t.Cleanup(func() {
			_ = os.Unsetenv(cnf.Application.EnvPrefix + "HOME")
			_ = os.Unsetenv(cnf.Application.EnvPrefix + "TMP")
		})

		// Test defaults
		assert.Equal(t, "state.json", cnf.Application.UserStateFile)
		assert.Equal(t, true, cnf.Updates.Check)
		assert.Equal(t, 3600, cnf.Updates.CheckInterval)
		assert.Equal(t, cnf.Application.UserConfigDir, cnf.Application.WritableUserDir)
		assert.Equal(t, "example-cli-tmp", cnf.Application.TempSubDir)
		assert.Equal(t, "platform", cnf.Service.ProjectConfigFlavor)

		homeDir, err := cnf.HomeDir()
		require.NoError(t, err)
		assert.Equal(t, tempDir, homeDir)

		writableDir, err := cnf.WritableUserDir()
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(homeDir, cnf.Application.WritableUserDir), writableDir)

		d, err := cnf.TempDir()
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(tempDir, "tmp", cnf.Application.TempSubDir), d)
	})
}

func TestIsEmbedded(t *testing.T) {
	t.Setenv("CLI_CONFIG_FILE", "")
	b, err := config.LoadYAML()
	require.NoError(t, err)
	cnf, err := config.FromYAML(b)
	require.NoError(t, err)
	assert.True(t, cnf.IsEmbedded())

	cnf, err = config.FromYAML([]byte(validConfig))
	require.NoError(t, err)
	assert.False(t, cnf.IsEmbedded())
}

func TestEmbedded(t *testing.T) {
	t.Setenv("CLI_CONFIG_FILE", "")
	b, err := config.LoadYAML()
	require.NoError(t, err)
	cnf, err := config.FromYAML(b)
	require.NoError(t, err)
	assert.Same(t, cnf, cnf.Embedded())

	altCnf, err := config.FromYAML([]byte(validConfig))
	require.NoError(t, err)
	embedded := altCnf.Embedded()
	assert.True(t, embedded.IsEmbedded())
	assert.Equal(t, cnf.Wrapper, embedded.Wrapper)
	assert.Equal(t, cnf.Application.Executable, embedded.Application.Executable)
	assert.Empty(t, altCnf.Wrapper.GitHubRepo)
}

func TestUserAgent(t *testing.T) {
	cnf := &config.Config{}
	cnf.Application.Name = "Test CLI"
	assert.Equal(t, fmt.Sprintf("Test-CLI/%s (%s; %s; Go)", config.Version, runtime.GOOS, runtime.GOARCH), cnf.UserAgent())

	cnf.API.UserAgent = "{APP_NAME}/{VERSION}"
	assert.Equal(t, "Test CLI/"+config.Version, cnf.UserAgent())
}
