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

// TestAuthAPIToken_EnvTokenChanges checks that each API token set via the environment gets its own tokens, which are
// reused, and that the user's session is used when it is unset.
func TestAuthAPIToken_EnvTokenChanges(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken": "default-access", "refreshToken": "default-refresh", "tokenType": "bearer",
		"expires": time.Now().Add(time.Hour).Unix(),
	})
	token := func(apiToken string) string {
		f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN="+apiToken)
		return f.Run("auth:token", "--no-warn")
	}

	assert.Equal(t, "access-token-1", token(mockapi.ValidAPITokens[0]))
	assert.Equal(t, "access-token-2", token(mockapi.ValidAPITokens[1]))
	assert.Equal(t, "access-token-1", token(mockapi.ValidAPITokens[0]))
	assert.Equal(t, "access-token-2", token(mockapi.ValidAPITokens[1]))
	assert.Equal(t, 2, authServer.APITokenExchanges())

	assert.Equal(t, "default-access", token(""))
	assert.Empty(t, authServer.RevokedTokens())
}

// TestAuthAPIToken_EnvTokenOverridesStored checks that an API token set via the environment is used instead of one
// saved by auth:api-token-login, in Go and PHP commands.
func TestAuthAPIToken_EnvTokenOverridesStored(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", Username: "testuser", Email: "test@example.com"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=", EnvPrefix+"API_WRITE_USER_SSH_CONFIG=0")
	_, stderr, err := f.RunInteractive(mockapi.ValidAPITokens[1]+"\n", "auth:api-token-login")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, "access-token-2", f.Run("auth:token", "--no-warn"))

	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN="+mockapi.ValidAPITokens[0])
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
	assert.Equal(t, "u1\n", f.Run("auth:info", "id", "--refresh"))
	assert.Equal(t, "Bearer access-token-1", lastRequest(t, apiHandler, "GET", "/users/me").Header.Get("Authorization"))

	// The stored token is used again when the environment variable is unset.
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	assert.Equal(t, "access-token-2", f.Run("auth:token", "--no-warn"))
}

// TestAuthAPIToken_LogoutWithEnvToken checks that logging out revokes the tokens of an API token set via the
// environment, so that the next command exchanges it again.
func TestAuthAPIToken_LogoutWithEnvToken(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))

	_, stderr, err := f.RunCombinedOutput("auth:logout")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "Warning: an API token is set via config")
	assert.ElementsMatch(t, []string{"access-token-1", "refresh-token-1"}, authServer.RevokedTokens())

	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
	assert.Equal(t, 2, authServer.APITokenExchanges())
}

// TestAuthAPITokenLogin_ReplacesSession checks that an API token login revokes the session's previous tokens: those of
// a browser login, then those of a previous API token.
func TestAuthAPITokenLogin_ReplacesSession(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newGoAuthCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=", EnvPrefix+"API_WRITE_USER_SSH_CONFIG=0")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken": "default-access", "refreshToken": "default-refresh", "tokenType": "bearer",
		"expires": time.Now().Add(time.Hour).Unix(),
	})

	_, stderr, err := f.RunInteractive(mockapi.ValidAPITokens[0]+"\n", "auth:api-token-login")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.ElementsMatch(t, []string{"default-access", "default-refresh"}, authServer.RevokedTokens())
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))

	_, stderr, err = f.RunInteractive(mockapi.ValidAPITokens[1]+"\n", "auth:api-token-login")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.ElementsMatch(t, []string{"default-access", "default-refresh", "access-token-1", "refresh-token-1"},
		authServer.RevokedTokens())
	assert.Equal(t, "access-token-2", f.Run("auth:token", "--no-warn"))
	assert.Equal(t, 2, authServer.APITokenExchanges())
	assert.JSONEq(t, `{"logged_in": true, "session_ids": ["default"], "has_stored_api_token": true}`,
		f.Run("auth:internal", "status"))
}

// TestAuthAPIToken_EmptyTokenFile checks that an empty token file does not hide a stored API token.
func TestAuthAPIToken_EmptyTokenFile(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=", EnvPrefix+"API_WRITE_USER_SSH_CONFIG=0")
	_, stderr, err := f.RunInteractive(mockapi.ValidAPITokens[1]+"\n", "auth:api-token-login")
	require.NoError(t, err, "stderr: %s", stderr)

	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("\n"), 0o600))
	f.extraEnv = append(f.extraEnv, EnvPrefix+"API_TOKEN_FILE="+tokenFile)
	assert.Equal(t, "access-token-2", f.Run("auth:token", "--no-warn"))
	assert.Equal(t, "u1\n", f.Run("auth:info", "id", "--refresh"))
}
