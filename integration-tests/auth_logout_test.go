package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAuthLogout_Single(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stderr, err := f.RunCombinedOutput("auth:logout")
	require.NoError(t, err)
	assert.Contains(t, stderr, "logged out")
	// The factory sets an API token via the environment, which cannot be removed.
	assert.Contains(t, stderr, "Warning: an API token is set via config")
}

// TestAuthLogout_SingleRevokesSession checks that logging out of the current session revokes its tokens and deletes
// its files.
func TestAuthLogout_SingleRevokesSession(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newCommandFactory(t, "", authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken": "default-access", "refreshToken": "default-refresh", "tokenType": "bearer",
		"expires": time.Now().Add(time.Hour).Unix(),
	})
	sessDir := filepath.Join(f.home, ".platform-test-cli", ".session")
	certFile := filepath.Join(sessDir, "sess-cli-default", "ssh", "id_ed25519-cert.pub")
	require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0o600))

	_, stderr, err := f.RunCombinedOutput("auth:logout")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "You are now logged out.")
	assert.NotContains(t, stderr, "Warning: an API token is set via config")
	assert.NotContains(t, stderr, "Other sessions exist")

	assert.ElementsMatch(t, []string{"default-access", "default-refresh"}, authServer.RevokedTokens())
	assert.NoFileExists(t, filepath.Join(sessDir, "sess-default", "sess-default.json"))
	assert.NoFileExists(t, certFile)
	assert.NoFileExists(t, filepath.Join(f.home, ".platform-test-cli", "auth", "default.json"))

	_, _, err = f.RunCombinedOutput("auth:token", "--no-warn")
	assertExitCode(t, 3, err)
}

// TestAuthLogout_OtherWithoutOtherSessions checks that --other keeps the current session.
func TestAuthLogout_OtherWithoutOtherSessions(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newCommandFactory(t, "", authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken": "default-access", "refreshToken": "default-refresh", "tokenType": "bearer",
		"expires": time.Now().Add(time.Hour).Unix(),
	})

	_, stderr, err := f.RunCombinedOutput("auth:logout", "--other")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "The current session ID is: default")
	assert.Contains(t, stderr, "No other sessions exist.")
	assert.Empty(t, authServer.RevokedTokens())
	assert.Equal(t, "default-access", f.Run("auth:token", "--no-warn"))
}

// TestAuthLogout_OtherSessionsHint: when other sessions still exist after a single logout,
// the hint to log out of all sessions is shown.
func TestAuthLogout_OtherSessionsHint(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newCommandFactory(t, "", authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")

	// Pre-populate two sessions so "other sessions exist" branch fires.
	future := time.Now().Add(time.Hour).Unix()
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken": "token-default", "tokenType": "bearer", "expires": future,
	})
	writeOAuthSession(t, f.home, "other", map[string]any{
		"accessToken": "token-other", "tokenType": "bearer", "expires": future,
	})

	_, stderr, err := f.RunCombinedOutput("auth:logout")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "Other sessions exist. Log out of all sessions with: platform-test logout --all")
}

func TestAuthLogout_All(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stderr, err := f.RunCombinedOutput("auth:logout", "--all")
	require.NoError(t, err)
	assert.Contains(t, stderr, "logged out")
}

func TestAuthLogout_Other(t *testing.T) {
	t.Parallel()
	// Use the auth server so the revoke POST has a valid endpoint to hit.
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newCommandFactory(t, "", authServer.URL)
	// With an API token set, each session would use the token's own session ID.
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")

	// Pre-populate two sessions: "default" (current) and "other".
	future := time.Now().Add(time.Hour).Unix()
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken": "token-default",
		"tokenType":   "bearer",
		"expires":     future,
	})
	writeOAuthSession(t, f.home, "other", map[string]any{
		"accessToken": "token-other",
		"tokenType":   "bearer",
		"expires":     future,
	})

	_, stderr, err := f.RunCombinedOutput("auth:logout", "--other")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "All other sessions have been deleted")

	// The "other" session's tokens are revoked and its files deleted.
	assert.Contains(t, authServer.RevokedTokens(), "token-other")
	assert.NotContains(t, authServer.RevokedTokens(), "token-default")
	sessDir := filepath.Join(f.home, ".platform-test-cli", ".session")
	assert.NoFileExists(t, filepath.Join(sessDir, "sess-other", "sess-other.json"))
	assert.NoDirExists(t, filepath.Join(sessDir, "sess-cli-other"))

	if goAuthMode() {
		// The sessions were migrated from the legacy CLI's storage, which was then deleted.
		assert.NoFileExists(t, filepath.Join(sessDir, "sess-default", "sess-default.json"))
		authDir := filepath.Join(f.home, ".platform-test-cli", "auth")
		assert.FileExists(t, filepath.Join(authDir, "default.json"))
		assert.NoFileExists(t, filepath.Join(authDir, "other.json"))
	} else {
		// "default" session file must still exist. An empty sess-other directory may be left behind.
		assert.FileExists(t, filepath.Join(sessDir, "sess-default", "sess-default.json"))
	}
}

// TestAuthLogout_RevokeFailure checks that a logout retries a revocation once after a transient error, and deletes
// the session with a warning if revocation fails.
func TestAuthLogout_RevokeFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		failures    int
		status      int
		wantRevoked []string
		wantStderr  []string
	}{
		{
			name: "retried", failures: 1, status: http.StatusServiceUnavailable,
			wantRevoked: []string{"default-refresh", "default-access"},
		},
		{
			name: "failed", failures: 3, status: http.StatusServiceUnavailable,
			wantRevoked: []string{"default-access"},
			wantStderr:  []string{"Warning: failed to revoke the refresh token: token revocation failed with status 503"},
		},
		{
			name: "not retried", failures: 2, status: http.StatusBadRequest, wantRevoked: []string{},
			wantStderr: []string{
				"Warning: failed to revoke the refresh token: token revocation failed with status 400",
				"Warning: failed to revoke the access token: token revocation failed with status 400",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()
			authServer.SetRevokeFailures(c.failures, c.status)

			f := newGoAuthCommandFactory(t, "", authServer.URL)
			f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
			writeOAuthSession(t, f.home, "default", map[string]any{
				"accessToken": "default-access", "refreshToken": "default-refresh",
				"expires": time.Now().Add(time.Hour).Unix(),
			})

			_, stderr, err := f.RunCombinedOutput("auth:logout")
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Contains(t, stderr, "You are now logged out.")
			assert.Equal(t, c.wantRevoked, authServer.RevokedTokens())
			for _, s := range c.wantStderr {
				assert.Contains(t, stderr, s)
			}
			if len(c.wantStderr) == 0 {
				assert.NotContains(t, stderr, "Warning")
			}
			assert.NoFileExists(t, filepath.Join(f.home, ".platform-test-cli", "auth", "default.json"))
		})
	}
}
