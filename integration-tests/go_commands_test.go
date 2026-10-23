package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersion(t *testing.T) {
	t.Parallel()
	f := newCommandFactory(t, "", "")

	assert.True(t, strings.HasPrefix(f.Run("version"), "Platform Test CLI "))

	output := f.Run("version", "-v")
	assert.Contains(t, output, "Embedded PHP version ")
	assert.Contains(t, output, "Commit ")
}

func TestConfigInstall(t *testing.T) {
	t.Parallel()
	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)
	altConfig := strings.NewReplacer(
		"name: 'Platform Test CLI'", "name: 'Alt Test CLI'",
		"executable: 'platform-test'", "executable: 'alt-test'",
	).Replace(string(baseConfig))
	configServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/alt.yaml":
			_, _ = w.Write([]byte(altConfig))
		case "/same.yaml":
			_, _ = w.Write(baseConfig)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer configServer.Close()

	f := newCommandFactory(t, "", "")
	binDir, configDir := t.TempDir(), t.TempDir()
	// The shim runs this executable (--absolute), not one found on the PATH.
	args := []string{"config:install", "--bin-dir", binDir, "--config-dir", configDir, "--absolute"}

	_, stdErr, err := f.RunCombinedOutput(append(args, configServer.URL+"/same.yaml")...)
	assert.Error(t, err)
	assert.Contains(t, stdErr, "cannot install config for same executable name as this program: platform-test")

	_, stdErr, err = f.RunCombinedOutput(append(args, configServer.URL+"/missing.yaml")...)
	assert.Error(t, err)
	assert.Contains(t, stdErr, "received unexpected response code 404")

	_, stdErr, err = f.RunCombinedOutput(append(args, configServer.URL+"/alt.yaml")...)
	require.NoError(t, err)
	assert.Contains(t, stdErr, "The files have been saved successfully.")
	assert.Contains(t, stdErr, "Add the following directory to your")

	b, err := os.ReadFile(filepath.Join(configDir, "alt-test.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(b), "executable: 'alt-test'")
	assert.Contains(t, string(b), "url: "+configServer.URL+"/alt.yaml")

	shim := filepath.Join(binDir, "alt-test")
	if runtime.GOOS == "windows" {
		shim += ".bat"
	}
	cmd := exec.Command(shim, "--version")
	cmd.Env = testEnv(f.home)
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(output), "Alt Test CLI "), string(output))
}

func TestProjectConvert(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("lib-sun writes source.root with a Windows path separator, and drops or moves comments")
	}
	// The command is only available in CLIs for Upsun (Flex) configuration.
	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := strings.Replace(string(baseConfig), "\nservice:\n", "\nservice:\n  project_config_flavor: 'upsun'\n", 1)
	require.NoError(t, os.WriteFile(configFile, []byte(config), 0o600))

	f := newCommandFactory(t, "", "")
	f.useConfig(configFile)
	f.dir = t.TempDir()
	require.NoError(t, os.CopyFS(f.dir, os.DirFS("../internal/convert/testdata/platformsh")))

	_, stdErr, err := f.RunCombinedOutput("convert", "--provider", "other")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "only the 'platformsh' provider is currently supported")
	assert.NoFileExists(t, filepath.Join(f.dir, ".upsun", "config.yaml"))

	f.Run("project:convert")
	expected, err := os.ReadFile("../internal/convert/testdata/platformsh/.upsun/config-ref.yaml")
	require.NoError(t, err)
	actual, err := os.ReadFile(filepath.Join(f.dir, ".upsun", "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, normalizeNewlines(string(expected)), normalizeNewlines(string(actual)))
}
