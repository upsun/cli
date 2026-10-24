package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestAuthMigration checks that sessions saved by the legacy CLI are migrated, and the legacy copies deleted.
func TestAuthMigration(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	authServer.AddRefreshToken("legacy-refresh-token")
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newGoAuthCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")

	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "legacy-access-token",
		"tokenType":    "bearer",
		"expires":      time.Now().Add(-time.Hour).Unix(),
		"refreshToken": "legacy-refresh-token",
	})
	sessDir := filepath.Join(f.home, ".platform-test-cli", ".session")
	sshFile := filepath.Join(sessDir, "sess-cli-default", "ssh", "id_ed25519-cert.pub")
	require.NoError(t, os.WriteFile(sshFile, []byte("cert"), 0o600))
	// An API token saved by auth:api-token-login in the "work" session.
	require.NoError(t, os.MkdirAll(filepath.Join(sessDir, "sess-cli-work"), 0o700))
	apiTokenFile := filepath.Join(sessDir, "sess-cli-work", "api-token")
	require.NoError(t, os.WriteFile(apiTokenFile, []byte(mockapi.ValidAPITokens[0]), 0o600))

	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
	assert.False(t, authServer.ReuseDetected())

	authDir := filepath.Join(f.home, ".platform-test-cli", "auth")
	assert.FileExists(t, filepath.Join(authDir, ".migrated"))
	assert.FileExists(t, filepath.Join(authDir, "default.json"))
	assert.FileExists(t, filepath.Join(authDir, "work.json"))
	assert.NoDirExists(t, filepath.Join(sessDir, "sess-default"))
	assert.NoFileExists(t, apiTokenFile)
	assert.FileExists(t, sshFile, "SSH certificates must be kept")

	// The migrated API token is used in its session.
	f.extraEnv = append(f.extraEnv, EnvPrefix+"SESSION_ID=work")
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))

	// A legacy session written after the migration is not imported.
	writeOAuthSession(t, f.home, "late", map[string]any{
		"accessToken": "late-token",
		"expires":     time.Now().Add(time.Hour).Unix(),
	})
	f.extraEnv = append(f.extraEnv, EnvPrefix+"SESSION_ID=late")
	_, _, err := f.RunCombinedOutput("auth:token", "--no-warn")
	assertExitCode(t, 3, err)
}

// TestAuthRefresh_ManyExpiries runs Go and PHP commands in parallel across several token expiries.
// Each expiry must cause exactly one refresh, without reusing a rotated refresh token.
func TestAuthRefresh_ManyExpiries(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	// Tokens are refreshed 2 minutes before they expire, so they are usable for 2 seconds.
	const usable = 2 * time.Second
	authServer.SetTokenLifetime(2*time.Minute + usable)
	authServer.AddRefreshToken("initial-refresh-token")

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newGoAuthCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	f.dir = t.TempDir()
	getCommandName(t)
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "expired-token",
		"tokenType":    "bearer",
		"expires":      time.Now().Add(-time.Hour).Unix(),
		"refreshToken": "initial-refresh-token",
	})
	// Migrate before running commands concurrently.
	f.Run("auth:token", "--no-warn")

	const (
		workers  = 6
		duration = 5 * time.Second
	)
	commands := [][]string{
		{"auth:token", "--no-warn"},
		{"auth:info", "id", "--refresh"},
	}
	start := time.Now()
	deadline := start.Add(duration)
	f.quiet = true
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		runs int
		errs []string
	)
	for i := range workers {
		wg.Go(func() {
			for time.Now().Before(deadline) {
				_, stderr, err := f.RunCombinedOutput(commands[i%len(commands)]...)
				mu.Lock()
				runs++
				if err != nil {
					errs = append(errs, stderr)
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	assert.Empty(t, errs)
	assert.False(t, authServer.ReuseDetected(), "a rotated refresh token was reused")
	// Commands started before the deadline may finish after it.
	expiries := int(time.Since(start)/usable) + 1
	refreshes := authServer.RefreshRequests()
	t.Logf("%d commands, %d refreshes", runs, refreshes)
	assert.GreaterOrEqual(t, refreshes, 2)
	assert.LessOrEqual(t, refreshes, expiries+1, "there must be one refresh per expiry")
}

// TestAuthRefresh_KilledWhileRefreshing checks that a process killed while holding the lock does not block others.
func TestAuthRefresh_KilledWhileRefreshing(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses SIGKILL")
	}
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	authServer.AddRefreshToken("initial-refresh-token")
	authServer.SetRefreshDelay(time.Minute)
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	f := newGoAuthCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "expired-token",
		"expires":      time.Now().Add(-time.Hour).Unix(),
		"refreshToken": "initial-refresh-token",
	})

	cmd := f.buildCommand("auth:token", "--no-warn")
	require.NoError(t, cmd.Start())
	require.Eventually(t, func() bool { return authServer.RefreshRequests() == 1 }, 20*time.Second, 50*time.Millisecond)
	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	authServer.SetRefreshDelay(0)
	start := time.Now()
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
	assert.Less(t, time.Since(start), 10*time.Second)
	assert.False(t, authServer.ReuseDetected())
}

// TestAuthRefresh_TransientError checks that server errors during a refresh keep the session.
func TestAuthRefresh_TransientError(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	authServer.AddRefreshToken("initial-refresh-token")
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	f := newGoAuthCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "expired-token",
		"expires":      time.Now().Add(-time.Hour).Unix(),
		"refreshToken": "initial-refresh-token",
	})

	// A failure after the request was sent is not retried.
	authServer.SetRefreshFailures(1)
	_, stderr, err := f.RunCombinedOutput("auth:token", "--no-warn")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "failed to refresh the access token")
	assert.Equal(t, 1, authServer.RefreshRequests())

	// The session is kept, so the next refresh succeeds.
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
	assert.Equal(t, 2, authServer.RefreshRequests())
	assert.False(t, authServer.ReuseDetected())
}

// TestAuthRefresh_InvalidGrantClearsSessionFiles checks that an expired session's files are deleted.
func TestAuthRefresh_InvalidGrantClearsSessionFiles(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newGoAuthCommandFactory(t, "", authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "expired-token",
		"expires":      time.Now().Add(-time.Hour).Unix(),
		"refreshToken": "unknown-refresh-token",
	})
	dir := filepath.Join(f.home, ".platform-test-cli")
	certFile := filepath.Join(dir, ".session", "sess-cli-default", "ssh", "id_ed25519-cert.pub")
	sshConfig := filepath.Join(dir, "ssh", "session.config")
	require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Dir(sshConfig), 0o700))
	require.NoError(t, os.WriteFile(sshConfig, []byte("config"), 0o600))

	_, stderr, err := f.RunCombinedOutput("auth:token", "--no-warn")
	assertExitCode(t, 3, err)
	assert.Contains(t, stderr, "logged out")
	assert.NoFileExists(t, certFile)
	assert.NoFileExists(t, sshConfig)
}

// TestAuthStepUp checks the message for a step-up authentication challenge (RFC 9470) in a PHP command.
func TestAuthStepUp(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1"})
	apiHandler.RequireStepUp([]string{"mfa"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newGoAuthCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken": "valid-token",
		"expires":     time.Now().Add(time.Hour).Unix(),
	})

	_, stderr, err := f.RunCombinedOutput("auth:info", "id", "--refresh")
	assertExitCode(t, 3, err)
	assert.Contains(t, stderr, "Multi-factor authentication (MFA) is required.")
	assert.Contains(t, stderr, "platform-test login --method mfa")
}

// TestAuthInternal checks the JSON output of the hidden command used by the legacy CLI.
func TestAuthInternal(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newGoAuthCommandFactory(t, "", authServer.URL)
	out := f.Run("auth:internal", "status")
	assert.JSONEq(t, `{"logged_in": true, "session_ids": [], "has_stored_api_token": false}`, out)

	out = f.Run("auth:internal", "token")
	assert.Contains(t, out, `"access_token":"access-token-1"`)

	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	stdout, stderr, err := f.RunCombinedOutput("auth:internal", "token")
	assertExitCode(t, 3, err)
	assert.Empty(t, stdout)
	assert.Equal(t, "{}", strings.TrimSpace(stderr))
}

// TestAuthInternalCommandsHidden checks that the internal legacy commands are hidden, and not run via abbreviations.
func TestAuthInternalCommandsHidden(t *testing.T) {
	t.Parallel()
	f := newGoAuthCommandFactory(t, "", "")

	var list struct {
		Commands []struct {
			Name   string `json:"name"`
			Hidden bool   `json:"hidden"`
		} `json:"commands"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.Run("list", "--all", "--format=json")), &list))
	hidden := map[string]bool{}
	for _, c := range list.Commands {
		hidden[c.Name] = c.Hidden
	}
	for _, name := range []string{"auth:export-sessions", "auth:post-login"} {
		h, ok := hidden[name]
		assert.True(t, ok && h, "%s must be listed as hidden", name)
	}

	_, stderr, err := f.RunCombinedOutput("auth:ex")
	assert.Error(t, err)
	assert.Contains(t, stderr, `The command "auth:ex" does not exist.`)
}

// TestErrorOutput_QuietOverride checks that --verbose and --debug override --quiet for errors.
func TestErrorOutput_QuietOverride(t *testing.T) {
	t.Parallel()
	f := newGoAuthCommandFactory(t, "", "")
	cases := []struct {
		args      []string
		wantError bool
	}{
		{[]string{"-q"}, false},
		{[]string{"-qv"}, true},
		{[]string{"-q", "--debug"}, true},
	}
	for _, c := range cases {
		_, stderr, err := f.RunCombinedOutput(append(append([]string{"auth:token"}, c.args...), "--unknown")...)
		assert.Error(t, err)
		if c.wantError {
			assert.Contains(t, stderr, "Error:", "args: %v", c.args)
		} else {
			assert.NotContains(t, stderr, "Error:", "args: %v", c.args)
		}
	}
}

// TestAuthChannel checks that the legacy CLI gets tokens from the wrapper over the inherited socket.
func TestAuthChannel(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the legacy CLI runs auth:internal on Windows")
	}
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newGoAuthCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"DEBUG=1")
	stdout, stderr, err := f.RunCombinedOutput("auth:info", "id", "--refresh")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, "u1\n", stdout)
	assert.Contains(t, stderr, "Auth channel request: token")
}

// TestAuthChannel_TokenNotInherited checks that processes started by the legacy CLI, such as Git hooks, do not inherit
// the API token, and that a CLI command run by them does not use other credentials instead.
func TestAuthChannel_TokenNotInherited(t *testing.T) {
	t.Parallel()
	if !goAuthMode() {
		t.Skip("with legacy auth, the legacy CLI needs the API token")
	}
	p := setUpGitProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil))
	repo := repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)
	p.f.dir = repo
	// A stored session, which may be for another user.
	writeOAuthSession(t, p.f.home, "default", map[string]any{
		"accessToken": "stored-token", "expires": time.Now().Add(time.Hour).Unix(),
	})
	tmp := t.TempDir()
	envFile, nestedFile := filepath.Join(tmp, "env"), filepath.Join(tmp, "nested")
	hooksDir := filepath.Join(repo, ".git", "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	hook := []byte("#!/bin/sh\nenv > '" + envFile + "'\n" +
		`"$` + EnvPrefix + `WRAPPER_EXECUTABLE" auth:token -W > '` + nestedFile + `' 2>&1; echo "exit $?" >> '` +
		nestedFile + "'\n")
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "pre-push"), hook, 0o755))
	runGit(t, repo, "config", "core.hooksPath", hooksDir)

	_, stderr, err := p.f.RunCombinedOutput("push", "-y")
	require.NoError(t, err, "stderr: %s", stderr)
	b, err := os.ReadFile(envFile)
	require.NoError(t, err)
	// Only the relevant variables are compared, to avoid printing the environment.
	env := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if name, value, ok := strings.Cut(line, "="); ok &&
			slices.Contains([]string{EnvPrefix + "TOKEN", EnvPrefix + "WRAPPER_HAS_TOKEN", "CLI_SSH_NO_REFRESH"}, name) {
			env[name] = value
		}
	}
	assert.Equal(t, map[string]string{EnvPrefix + "WRAPPER_HAS_TOKEN": "1", "CLI_SSH_NO_REFRESH": "1"}, env)

	b, err = os.ReadFile(nestedFile)
	require.NoError(t, err)
	assert.Contains(t, string(b), "not passed to the processes it starts")
	assert.Contains(t, string(b), "exit 1")
	assert.NotContains(t, string(b), "stored-token")
}

// TestAuthAccessTokenPrecedence checks that an access token set via the environment is used in PHP commands instead of
// one in the user config file, also when the wrapper removes it from PHP's environment.
func TestAuthAccessTokenPrecedence(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.Get("/curl-test", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("success")) })
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, "")
	configDir := filepath.Join(f.home, ".platform-test-cli")
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"),
		[]byte("api:\n  access_token: from-config\n"), 0o600))
	f.extraEnv = append(f.extraEnv, EnvPrefix+"API_TOKEN=from-env")

	assert.Equal(t, "success", f.Run("api:curl", "/curl-test"))
	assert.Equal(t, "Bearer from-env", lastRequest(t, apiHandler, "GET", "/curl-test").Header.Get("Authorization"))
}
