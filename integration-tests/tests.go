// Package tests contains integration tests, which run the CLI as a shell command and verify its output.
//
// A TEST_CLI_PATH environment variable can be provided to override the path to a
// CLI executable. It defaults to a built binary in the dist/ directory.
//
// Tests run in directories under INTEGRATION_TESTS_TMPDIR, which defaults to a
// subdirectory of the user cache directory. It must not be inside a Git repository.
package tests

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

var _validatedCommand string

func getCommandName(t *testing.T) string {
	if testing.Short() {
		t.Skip("skipping integration test due to -short flag")
	}
	if _validatedCommand != "" {
		return _validatedCommand
	}
	candidate := os.Getenv("TEST_CLI_PATH")
	if candidate != "" {
		_, err := os.Stat(candidate)
		require.NoError(t, err)
	} else {
		// Look for built binaries in dist/ directory (from make single or make snapshot).
		matches, _ := filepath.Glob("../dist/*/upsun")
		if len(matches) == 0 {
			matches, _ = filepath.Glob("../dist/*/platform")
		}
		if len(matches) == 0 {
			t.Skip("skipping integration tests: CLI not found in dist/ directory (run 'make single' to build)")
			return ""
		}
		c, err := filepath.Abs(matches[0])
		require.NoError(t, err)
		candidate = c
	}
	versionCmd := exec.Command(candidate, "--version")
	versionCmd.Dir = t.TempDir()
	versionCmd.Env = testEnv(t.TempDir())
	output, err := versionCmd.Output()
	require.NoError(t, err, "running '--version' must succeed under the CLI at: %s", candidate)
	require.Contains(t, string(output), "Platform Test CLI ")
	t.Logf("Validated CLI command %s", candidate)
	_validatedCommand = candidate
	return _validatedCommand
}

type cmdFactory struct {
	t        *testing.T
	apiURL   string
	authURL  string
	extraEnv []string
	dir      string // Working directory; defaults to a per-test temporary directory.
	home     string // CLI home directory; a per-test temporary directory.
	stdin    io.Reader
}

func newCommandFactory(t *testing.T, apiURL, authURL string) *cmdFactory {
	return &cmdFactory{t: t, apiURL: apiURL, authURL: authURL, home: t.TempDir()}
}

// Run runs a command, asserts that it did not error, and returns its normal (stdout) output.
func (f *cmdFactory) Run(args ...string) string {
	cmd := f.buildCommand(args...)
	f.t.Log("Running:", cmd)
	b, err := cmd.Output()
	require.NoError(f.t, err)
	return string(b)
}

// RunCombinedOutput runs a command and returns its stdout, stderr and the error.
func (f *cmdFactory) RunCombinedOutput(args ...string) (stdOut, stdErr string, err error) {
	cmd := f.buildCommand(args...)
	var stdOutBuffer bytes.Buffer
	var stdErrBuffer bytes.Buffer
	cmd.Stdout = &stdOutBuffer
	cmd.Stderr = &stdErrBuffer
	if testing.Verbose() {
		cmd.Stderr = io.MultiWriter(&stdErrBuffer, os.Stderr)
	}
	f.t.Log("Running:", cmd)
	err = cmd.Run()
	return stdOutBuffer.String(), stdErrBuffer.String(), err
}

// RunInteractive runs a command with stdin piped from stdinInput. It keeps
// the legacy CLI in interactive mode under a pipe by setting SHELL_INTERACTIVE
// and stripping NO_INTERACTION.
func (f *cmdFactory) RunInteractive(stdinInput string, args ...string) (stdOut, stdErr string, err error) {
	cmd := f.buildCommand(args...)
	newEnv := make([]string, 0, len(cmd.Env)+1)
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, EnvPrefix+"NO_INTERACTION=") {
			continue
		}
		newEnv = append(newEnv, e)
	}
	newEnv = append(newEnv, "SHELL_INTERACTIVE=1")
	cmd.Env = newEnv
	cmd.Stdin = strings.NewReader(stdinInput)
	var stdOutBuffer bytes.Buffer
	var stdErrBuffer bytes.Buffer
	cmd.Stdout = &stdOutBuffer
	cmd.Stderr = &stdErrBuffer
	if testing.Verbose() {
		cmd.Stderr = io.MultiWriter(&stdErrBuffer, os.Stderr)
	}
	f.t.Log("Running (interactive):", cmd)
	err = cmd.Run()
	return stdOutBuffer.String(), stdErrBuffer.String(), err
}

func (f *cmdFactory) buildCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(getCommandName(f.t), args...)
	if f.dir == "" {
		f.dir = f.t.TempDir()
	}
	if f.home == "" {
		f.home = f.t.TempDir()
	}
	cmd.Env = testEnv(f.home)
	cmd.Dir = f.dir
	if testing.Verbose() {
		cmd.Stderr = os.Stderr
	}
	if f.apiURL != "" {
		cmd.Env = append(cmd.Env, EnvPrefix+"API_BASE_URL="+f.apiURL)
	}
	if f.authURL != "" {
		cmd.Env = append(cmd.Env, EnvPrefix+"API_AUTH_URL="+f.authURL, EnvPrefix+"TOKEN="+mockapi.ValidAPITokens[0])
	}
	cmd.Env = append(cmd.Env, f.extraEnv...)
	if f.stdin != nil {
		cmd.Stdin = f.stdin
	}
	return cmd
}

func assertTrimmed(t *testing.T, expected, actual string) {
	assert.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(actual))
}

const EnvPrefix = "TEST_CLI_"

func testEnv(home string) []string {
	configPath, err := filepath.Abs("config.yaml")
	if err != nil {
		panic(err)
	}
	return append(
		os.Environ(),
		"COLUMNS=120",
		"CLI_CONFIG_FILE="+configPath,
		EnvPrefix+"NO_INTERACTION=1",
		EnvPrefix+"VERSION=1.0.0",
		EnvPrefix+"HOME="+home,
		"TZ=UTC",
	)
}

// assertExitCode asserts that a command failed with the given exit code.
func assertExitCode(t *testing.T, expected int, err error) {
	t.Helper()
	var exitErr *exec.ExitError
	if assert.ErrorAs(t, err, &exitErr) {
		assert.Equal(t, expected, exitErr.ExitCode())
	}
}

// fakeBrowser makes the CLI detect a display and a browser, which it requires before offering a browser login.
func (f *cmdFactory) fakeBrowser() {
	f.t.Helper()
	if runtime.GOOS == "windows" {
		f.t.Skip("the fake browser is a shell script")
	}
	dir := f.t.TempDir()
	require.NoError(f.t, os.WriteFile(filepath.Join(dir, "xdg-open"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	f.extraEnv = append(f.extraEnv, "DISPLAY=:0", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakeBrowserThatLogsIn makes the CLI detect a display and a browser, which completes the login flow with the mock
// auth server by following its redirects.
func (f *cmdFactory) fakeBrowserThatLogsIn() {
	f.t.Helper()
	if runtime.GOOS == "windows" {
		f.t.Skip("the fake browser is a shell script")
	}
	dir := f.t.TempDir()
	script := "#!/bin/sh\nexec curl -fsSL -o /dev/null \"$1\"\n"
	require.NoError(f.t, os.WriteFile(filepath.Join(dir, "xdg-open"), []byte(script), 0o755))
	f.extraEnv = append(f.extraEnv, "DISPLAY=:0", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// waitForServer retries a GET request until the server responds.
func waitForServer(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	var resp *http.Response
	require.Eventually(t, func() bool {
		r, err := client.Get(url) //nolint:noctx,bodyclose // The caller closes the body.
		if err != nil {
			return false
		}
		resp = r
		return true
	}, 10*time.Second, 50*time.Millisecond, "server did not respond: %s", url)
	return resp
}
