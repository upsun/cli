// Package tests contains integration tests, which run the CLI as a shell command and verify its output.
//
// A TEST_CLI_PATH environment variable can be provided to override the path to a
// CLI executable. It defaults to a built binary in the dist/ directory.
//
// Tests run in directories under INTEGRATION_TESTS_TMPDIR, which defaults to a
// subdirectory of the user cache directory. It must not be inside a Git repository.
//
// If INTEGRATION_TESTS_ARGS_LOG is set, the arguments of each command are appended to that file, as a JSON array per
// line, for the command coverage report (scripts/coverage-report).
package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

var (
	_validatedCommand   string
	_validatedCommandMu sync.Mutex
)

func getCommandName(t *testing.T) string {
	if testing.Short() {
		t.Skip("skipping integration test due to -short flag")
	}
	// Tests run in parallel: validate once.
	_validatedCommandMu.Lock()
	defer _validatedCommandMu.Unlock()
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
	quiet    bool // Don't log commands or echo their stderr, e.g. in stress tests.
}

func newCommandFactory(t *testing.T, apiURL, authURL string) *cmdFactory {
	return &cmdFactory{t: t, apiURL: apiURL, authURL: authURL, home: t.TempDir()}
}

// newGoAuthCommandFactory returns a factory for commands that use Go auth, whichever mode the tests run in.
func newGoAuthCommandFactory(t *testing.T, apiURL, authURL string) *cmdFactory {
	f := newCommandFactory(t, apiURL, authURL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"GO_AUTH=1")
	return f
}

// goAuthMode reports whether the tests run with Go auth.
func goAuthMode() bool {
	v := goAuthEnv()
	return v != "" && v != "0"
}

// Run runs a command, asserts that it did not error, and returns its normal (stdout) output.
func (f *cmdFactory) Run(args ...string) string {
	cmd := f.buildCommand(args...)
	var stdErrBuffer bytes.Buffer
	cmd.Stderr = &stdErrBuffer
	f.t.Log("Running:", cmd)
	b, err := cmd.Output()
	f.logStderr(stdErrBuffer.String())
	require.NoError(f.t, err)
	return normalizeNewlines(string(b))
}

// RunCombinedOutput runs a command and returns its stdout, stderr and the error.
func (f *cmdFactory) RunCombinedOutput(args ...string) (stdOut, stdErr string, err error) {
	cmd := f.buildCommand(args...)
	var stdOutBuffer bytes.Buffer
	var stdErrBuffer bytes.Buffer
	cmd.Stdout = &stdOutBuffer
	cmd.Stderr = &stdErrBuffer
	if !f.quiet {
		f.t.Log("Running:", cmd)
	}
	err = cmd.Run()
	if !f.quiet {
		f.logStderr(stdErrBuffer.String())
	}
	return normalizeNewlines(stdOutBuffer.String()), normalizeNewlines(stdErrBuffer.String()), err
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
	f.t.Log("Running (interactive):", cmd)
	err = cmd.Run()
	f.logStderr(stdErrBuffer.String())
	return normalizeNewlines(stdOutBuffer.String()), normalizeNewlines(stdErrBuffer.String()), err
}

// normalizeNewlines converts CRLF line endings to LF: the legacy CLI writes CRLF on Windows (PHP_EOL).
func normalizeNewlines(s string) string {
	if runtime.GOOS != "windows" {
		return s
	}
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func (f *cmdFactory) buildCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(getCommandName(f.t), args...)
	logArgs(f.t, args)
	if f.dir == "" {
		f.dir = f.t.TempDir()
	}
	if f.home == "" {
		f.home = f.t.TempDir()
	}
	cmd.Env = testEnv(f.home)
	cmd.Dir = f.dir
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

var argsLogMu sync.Mutex

// logArgs appends a command's arguments to the file named by INTEGRATION_TESTS_ARGS_LOG, if set.
func logArgs(t *testing.T, args []string) {
	path := os.Getenv("INTEGRATION_TESTS_ARGS_LOG")
	if path == "" {
		return
	}
	b, err := json.Marshal(args)
	require.NoError(t, err)
	argsLogMu.Lock()
	defer argsLogMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	require.NoError(t, err)
}

// logStderr logs a command's stderr to the test log, which is shown if the test fails or runs with -v.
func (f *cmdFactory) logStderr(s string) {
	if s != "" {
		f.t.Logf("stderr:\n%s", s)
	}
}

// useConfig sets a CLI config file. It also sets a private temporary directory, as
// the legacy CLI copies the config to a shared cache file, which parallel tests would overwrite.
func (f *cmdFactory) useConfig(path string) {
	f.extraEnv = append(f.extraEnv, "CLI_CONFIG_FILE="+path, EnvPrefix+"TMP="+f.t.TempDir())
}

func assertTrimmed(t *testing.T, expected, actual string) {
	assert.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(actual))
}

const EnvPrefix = "TEST_CLI_"

// goAuthEnv returns the value of TEST_CLI_GO_AUTH for commands: "1" (Go auth), unless it is set, e.g. to "0" to run
// the tests with legacy auth.
// TODO: remove legacy auth runs once legacy auth is no longer used.
func goAuthEnv() string {
	if v, ok := os.LookupEnv(EnvPrefix + "GO_AUTH"); ok {
		return v
	}
	return "1"
}

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
		EnvPrefix+"GO_AUTH="+goAuthEnv(),
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
	// The CLI uses "open" on macOS, and "xdg-open" on Linux.
	for _, name := range []string{"open", "xdg-open"} {
		require.NoError(f.t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	}
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
	// The CLI uses "open" on macOS, and "xdg-open" on Linux.
	for _, name := range []string{"open", "xdg-open"} {
		require.NoError(f.t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
	}
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

// syncBuffer is a buffer that is safe for concurrent use, e.g. to read a running command's output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
