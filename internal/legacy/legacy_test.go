package legacy

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
)

func TestMakeCmdIgnoresSystemPHPIni(t *testing.T) {
	w := &CLIWrapper{Config: &config.Config{}}
	w.Config.Application.Executable = "platform-test"

	cmd := w.makeCmd(context.Background(), []string{"help"}, t.TempDir())

	// The "-n" flag must be present and precede the phar path so that PHP
	// ignores any host php.ini before the script runs.
	idxN := slices.Index(cmd.Args, "-n")
	require.GreaterOrEqual(t, idxN, 1, "expected -n in PHP args: %v", cmd.Args)
	pharIdx := slices.IndexFunc(cmd.Args, func(s string) bool {
		return strings.HasSuffix(s, ".phar")
	})
	require.Greater(t, pharIdx, idxN, "-n must come before the phar path")
}

func TestLegacyCLI(t *testing.T) {
	if len(phar) == 0 || len(phpCLI) == 0 {
		t.Skip()
	}

	cnf := &config.Config{}
	cnf.Application.Name = "Test CLI"
	cnf.Application.Executable = "platform-test"
	cnf.Application.Slug = "test-cli"
	cnf.Application.EnvPrefix = "TEST_CLI_"
	cnf.Application.TempSubDir = "temp_sub_dir"

	tempDir := t.TempDir()

	_ = os.Setenv(cnf.Application.EnvPrefix+"TMP", tempDir)
	t.Cleanup(func() {
		_ = os.Unsetenv(cnf.Application.EnvPrefix + "TMP")
	})

	stdout := &bytes.Buffer{}
	stdErr := io.Discard
	if testing.Verbose() {
		stdErr = os.Stderr
	}

	testCLIVersion := "1.2.3"

	wrapper := &CLIWrapper{
		Stdout:             stdout,
		Stderr:             stdErr,
		Config:             cnf,
		Version:            testCLIVersion,
		DisableInteraction: true,
	}
	if testing.Verbose() {
		wrapper.DebugLogFunc = t.Logf
	}
	PHPVersion = "6.5.4"

	err := wrapper.Exec(context.Background(), "help")
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), "Displays help for a command")

	cacheDir, err := wrapper.cacheDir()
	require.NoError(t, err)

	pharPath, err := wrapper.PharPath()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(cacheDir, "platform-test.phar"), pharPath)

	stdout.Reset()
	err = wrapper.Exec(context.Background(), "--version")
	assert.NoError(t, err)
	assert.Equal(t, "Test CLI "+testCLIVersion, strings.TrimSuffix(stdout.String(), "\n"))

	// Simulate a host-PHP config that would cause warnings on a static binary
	// (as in Lando containers): an ini that tries to dlopen extensions which
	// the embedded static PHP cannot load. With "-n" passed to PHP, the host
	// ini must be ignored entirely and produce no startup warnings.
	iniPath := filepath.Join(tempDir, "php.ini")
	require.NoError(t, os.WriteFile(iniPath, []byte(
		"extension=pdo_mysql\nextension=opcache\nextension=bcmath\n",
	), 0o644))
	t.Setenv("PHPRC", iniPath)
	t.Setenv("PHP_INI_SCAN_DIR", tempDir)

	stdout.Reset()
	capturedErr := &bytes.Buffer{}
	wrapper.Stderr = capturedErr
	err = wrapper.Exec(context.Background(), "--version")
	wrapper.Stderr = stdErr
	assert.NoError(t, err)
	combined := stdout.String() + capturedErr.String()
	assert.NotContains(t, combined, "Unable to load dynamic library")
	assert.NotContains(t, combined, "Failed loading")
}
