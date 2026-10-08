package tests

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAuthLogout_Single(t *testing.T) {
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
}

// TestAuthLogout_OtherSessionsHint: when other sessions still exist after a single logout,
// the hint to log out of all sessions is shown.
func TestAuthLogout_OtherSessionsHint(t *testing.T) {
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
