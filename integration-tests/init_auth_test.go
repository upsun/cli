package tests

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// newInitRepo returns a command factory that runs in a new Git repository.
func newInitRepo(t *testing.T, apiURL, authURL string) *cmdFactory {
	f := newCommandFactory(t, apiURL, authURL)
	f.dir = t.TempDir()
	runGit(t, f.dir, "init", "--quiet")
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, "index.php"), []byte("<?php echo 'Hello';\n"), 0o600))
	return f
}

// TestInit_AIRequestIsAuthenticated checks that the AI configuration request is sent with the user's token, with Go
// auth and with legacy auth (which needs a JWT access token).
func TestInit_AIRequestIsAuthenticated(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the repository digest fails on Windows: whatsun opens its embedded rules with OS-specific paths")
	}
	for _, goAuth := range []string{"1", "0"} {
		t.Run("GO_AUTH="+goAuth, func(t *testing.T) {
			t.Parallel()
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()
			authServer.SetJWTAccessTokens(true)
			apiHandler := mockapi.NewHandler(t)
			apiHandler.Post("/ai/generate-configuration", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "5")
				w.WriteHeader(http.StatusTooManyRequests)
			})
			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newInitRepo(t, apiServer.URL, authServer.URL)
			f.extraEnv = append(f.extraEnv, EnvPrefix+"GO_AUTH="+goAuth)
			_, stderr, err := f.RunCombinedOutput("init", "--ai=true")
			assertExitCode(t, 1, err)
			assert.Contains(t, stderr, "rate limit exceeded: please try again after 5 seconds")
			header := lastRequest(t, apiHandler, "POST", "/ai/generate-configuration").Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			require.True(t, ok, "header: %s", header)
			assert.Equal(t, "access-token-1", jwtClaims(t, token)["jti"])
		})
	}
}

// jwtClaims decodes a JWT's claims, without verifying it.
func jwtClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "token: %s", token)
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	return jsonObject(t, string(b))
}

// TestInit_LoginRequired checks that init reports that login is required before sending the AI configuration request.
func TestInit_LoginRequired(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newInitRepo(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=", EnvPrefix+"GO_AUTH=1")
	_, stderr, err := f.RunCombinedOutput("init", "--ai=true")
	assertExitCode(t, 3, err)
	assert.Contains(t, stderr, "Authentication is required.")
	assert.Empty(t, apiHandler.Requests("POST", "/ai/generate-configuration"))
}

// TestInit_StepUp checks that a step-up authentication challenge (RFC 9470) for the AI configuration request reports
// the required login, as in other commands.
func TestInit_StepUp(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the repository digest fails on Windows: whatsun opens its embedded rules with OS-specific paths")
	}
	cases := []struct {
		name     string
		apiToken bool
		want     string
	}{
		{name: "session", want: "Please log in by running:\n    platform-test login --method mfa"},
		{name: "API token", apiToken: true, want: "The API token may need to be re-created after enabling MFA."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()
			apiHandler := mockapi.NewHandler(t)
			apiHandler.RequireStepUp([]string{"mfa"})
			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newInitRepo(t, apiServer.URL, authServer.URL)
			f.extraEnv = append(f.extraEnv, EnvPrefix+"GO_AUTH=1")
			if !c.apiToken {
				f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
				writeOAuthSession(t, f.home, "default", map[string]any{
					"accessToken": "valid-token", "expires": time.Now().Add(time.Hour).Unix(),
				})
			}
			_, stderr, err := f.RunCombinedOutput("init", "--ai=true")
			assertExitCode(t, 3, err)
			assert.Contains(t, stderr, "Multi-factor authentication (MFA) is required.")
			assert.Contains(t, stderr, c.want)
			assert.NotContains(t, stderr, "Error:")
		})
	}
}

// TestInit_Unauthorized checks that a request rejected with a new token reports that login is required, as in the
// legacy CLI.
func TestInit_Unauthorized(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the repository digest fails on Windows: whatsun opens its embedded rules with OS-specific paths")
	}
	cases := []struct {
		name     string
		apiToken bool
		want     string
	}{
		{name: "session", want: "Please log in by running:\n    platform-test login"},
		{name: "API token", apiToken: true, want: "The API token was rejected. It may be invalid or revoked."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()
			apiHandler := mockapi.NewHandler(t)
			for range 2 {
				apiHandler.FailNext("POST", "/ai/generate-configuration", http.StatusUnauthorized,
					mockapi.Problem(http.StatusUnauthorized, "Invalid token."))
			}
			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newInitRepo(t, apiServer.URL, authServer.URL)
			f.extraEnv = append(f.extraEnv, EnvPrefix+"GO_AUTH=1")
			if !c.apiToken {
				f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
				authServer.AddRefreshToken("refresh-token")
				writeOAuthSession(t, f.home, "default", map[string]any{
					"accessToken": "valid-token", "expires": time.Now().Add(time.Hour).Unix(),
					"refreshToken": "refresh-token",
				})
			}
			_, stderr, err := f.RunCombinedOutput("init", "--ai=true")
			assertExitCode(t, 3, err)
			assert.Contains(t, stderr, c.want)
			assert.NotContains(t, stderr, "API error")
			assert.Len(t, apiHandler.Requests("POST", "/ai/generate-configuration"), 2)
		})
	}
}
