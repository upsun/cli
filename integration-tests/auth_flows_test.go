package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// newInteractiveLoginFactory returns a command factory for a user who is not logged in, in an interactive terminal
// with a browser that completes the login.
func newInteractiveLoginFactory(t *testing.T, apiURL, authURL string) *cmdFactory {
	f := newCommandFactory(t, apiURL, authURL)
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",
		EnvPrefix+"NO_INTERACTION=",
		"SHELL_INTERACTIVE=1",
		// Do not ask whether to write ~/.ssh/config.
		EnvPrefix+"API_WRITE_USER_SSH_CONFIG=0",
	)
	f.fakeBrowserThatLogsIn()
	return f
}

func newMyUserAPI(t *testing.T) *httptest.Server {
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", Username: "testuser", Email: "test@example.com"})
	return httptest.NewServer(apiHandler)
}

// TestAuthLogin_AcceptPromptInPHP accepts the login prompt in a PHP command, which runs the Go login.
func TestAuthLogin_AcceptPromptInPHP(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newInteractiveLoginFactory(t, apiServer.URL, authServer.URL)
	f.stdin = strings.NewReader("y\n")
	stdout, stderr, err := f.RunCombinedOutput("auth:info", "id")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, "u1\n", stdout)
	assert.Contains(t, stderr, "Log in via a browser?")
	assert.Contains(t, stderr, "You are logged in.")

	f.stdin = nil
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
}

// TestAuthLogin_AcceptPromptInGo accepts the login prompt in a Go command.
func TestAuthLogin_AcceptPromptInGo(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newInteractiveLoginFactory(t, apiServer.URL, authServer.URL)
	f.stdin = strings.NewReader("y\n")
	stdout, stderr, err := f.RunCombinedOutput("auth:token", "--no-warn")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, "access-token-1", stdout)
	assert.Contains(t, stderr, "Log in via a browser?")
}

// TestAuthBrowserLogin_ReplacesSession checks the effects of a login over an existing session.
func TestAuthBrowserLogin_ReplacesSession(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newInteractiveLoginFactory(t, apiServer.URL, authServer.URL)
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "old-access-token",
		"expires":      time.Now().Add(time.Hour).Unix(),
		"refreshToken": "old-refresh-token",
	})

	_, stderr, err := f.RunCombinedOutput("auth:browser-login", "--force")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "You are logged in.")
	assert.Contains(t, stderr, "Username: testuser\nEmail address: test@example.com")

	// The previous session was revoked and replaced.
	assert.Contains(t, authServer.RevokedTokens(), "old-access-token")
	assert.Contains(t, authServer.RevokedTokens(), "old-refresh-token")
	assert.FileExists(t, filepath.Join(f.home, ".platform-test-cli", "auth", "default.json"))
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))

	// An SSH certificate was generated.
	assert.FileExists(t, filepath.Join(f.home, ".platform-test-cli", ".session", "sess-cli-default", "ssh",
		"id_ed25519-cert.pub"))
}

// TestAuthRefresh_RejectedToken checks that PHP gets a new token from Go when the API rejects one before it expires.
func TestAuthRefresh_RejectedToken(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	authServer.SetUniqueAccessTokens(true)
	authServer.AddRefreshToken("initial-refresh-token")
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1"})
	apiHandler.RejectAccessToken("revoked-token")
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "revoked-token",
		"expires":      time.Now().Add(time.Hour).Unix(),
		"refreshToken": "initial-refresh-token",
	})

	assert.Equal(t, "u1\n", f.Run("auth:info", "id", "--refresh"))
	assert.Equal(t, 1, authServer.RefreshRequests())
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
	assert.False(t, authServer.ReuseDetected())
}

// TestAuthLogout_AllRevokesEverySession checks the effects of logging out of all sessions.
func TestAuthLogout_AllRevokesEverySession(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newCommandFactory(t, "", authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	future := time.Now().Add(time.Hour).Unix()
	for _, id := range []string{"default", "other"} {
		writeOAuthSession(t, f.home, id, map[string]any{
			"accessToken": id + "-access", "refreshToken": id + "-refresh", "expires": future,
		})
	}
	// Create lock files, as a refresh would.
	assert.Equal(t, "default-access", f.Run("auth:token", "--no-warn"))

	_, stderr, err := f.RunCombinedOutput("auth:logout", "--all")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "All sessions have been deleted.")
	assert.ElementsMatch(t,
		[]string{"default-access", "default-refresh", "other-access", "other-refresh"}, authServer.RevokedTokens())

	// Only the migration marker and lock files are kept.
	entries, err := os.ReadDir(filepath.Join(f.home, ".platform-test-cli", "auth"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.True(t, e.Name() == ".migrated" || strings.HasSuffix(e.Name(), ".lock"), "unexpected file: %s", e.Name())
	}
	assert.NoDirExists(t, filepath.Join(f.home, ".platform-test-cli", ".session"))

	_, _, err = f.RunCombinedOutput("auth:token", "--no-warn")
	assertExitCode(t, 3, err)
}

// TestAuthAPITokenLogin_Logout checks that logging out deletes a stored API token and revokes its tokens.
func TestAuthAPITokenLogin_Logout(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=", EnvPrefix+"API_WRITE_USER_SSH_CONFIG=0")
	_, stderr, err := f.RunInteractive(mockapi.ValidAPITokens[0]+"\n", "auth:api-token-login")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.JSONEq(t, `{"logged_in": true, "session_ids": ["default"], "has_stored_api_token": true}`,
		f.Run("auth:internal", "status"))

	f.Run("auth:logout")
	assert.Contains(t, authServer.RevokedTokens(), "refresh-token-1")
	assert.JSONEq(t, `{"logged_in": false, "session_ids": [], "has_stored_api_token": false}`,
		f.Run("auth:internal", "status"))
}

// TestSessionSwitch_UsesNewSession checks that PHP and Go use the same session after a switch.
func TestSessionSwitch_UsesNewSession(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	writeOAuthSession(t, f.home, "work", map[string]any{
		"accessToken": "work-token", "expires": time.Now().Add(time.Hour).Unix(),
	})

	// The switch reports the new session's account, which PHP gets with a token from Go.
	_, stderr, err := f.RunCombinedOutput("session:switch", "work")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "Username: testuser")

	assert.Equal(t, "work-token", f.Run("auth:token", "--no-warn"))
}
