package tests

import (
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

	// Factory sets TEST_CLI_AUTH_URL and TEST_CLI_TOKEN (both needed: AUTH_URL for revoke, TOKEN is just a warning).
	f := newCommandFactory(t, "", authServer.URL)

	// Pre-populate two sessions: "default" (current) and "other".
	future := time.Now().Add(time.Hour).Unix()
	writeOAuthSession(t, f.homeDir, "default", map[string]interface{}{
		"accessToken": "token-default",
		"tokenType":   "bearer",
		"expires":     future,
	})
	writeOAuthSession(t, f.homeDir, "other", map[string]interface{}{
		"accessToken": "token-other",
		"tokenType":   "bearer",
		"expires":     future,
	})

	_, stderr, err := f.RunCombinedOutput("auth:logout", "--other")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "All other sessions have been deleted")

	// "default" session file must still exist.
	defaultSessFile := filepath.Join(f.homeDir, ".platform-test-cli", ".session", "sess-default", "sess-default.json")
	assert.FileExists(t, defaultSessFile)

	// "other" session dir must be gone.
	otherSessDir := filepath.Join(f.homeDir, ".platform-test-cli", ".session", "sess-other")
	_, statErr := os.Stat(otherSessDir)
	assert.True(t, os.IsNotExist(statErr), "expected sess-other dir to be deleted, but it still exists")
}
