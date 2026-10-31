package tests

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestAuthLoginRequired_ConfiguredToken checks that no browser login is offered when an API token is set via config,
// because the browser login would refuse to run.
func TestAuthLoginRequired_ConfiguredToken(t *testing.T) {
	t.Parallel()
	// The parallel subtests run after this function returns, so servers are closed in cleanups.
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)
	apiServer := newMyUserAPI(t)
	t.Cleanup(apiServer.Close)
	stepUpHandler := mockapi.NewHandler(t)
	stepUpHandler.RequireStepUp([]string{"mfa"})
	stepUpServer := httptest.NewServer(stepUpHandler)
	t.Cleanup(stepUpServer.Close)

	mfaMessage := []string{
		"Multi-factor authentication (MFA) is required.",
		"The API token may need to be re-created after enabling MFA.",
	}
	invalidMessage := []string{
		"The API token is invalid.", "Authentication is required.", "Check the API token, or create a new one.",
	}

	cases := []struct {
		name    string
		goAuth  bool
		token   string
		apiURL  string
		args    []string
		message []string
	}{
		{
			name: "go auth, invalid token, PHP command", goAuth: true, token: "invalid-token",
			apiURL: apiServer.URL, args: []string{"projects"}, message: invalidMessage,
		},
		{
			name: "go auth, invalid token, Go command", goAuth: true, token: "invalid-token",
			apiURL: apiServer.URL, args: []string{"auth:token", "--no-warn"}, message: invalidMessage,
		},
		{
			name: "go auth, step-up, PHP command", goAuth: true, token: mockapi.ValidAPITokens[0],
			apiURL: stepUpServer.URL, args: []string{"projects"}, message: mfaMessage,
		},
		{
			name: "legacy auth, step-up, PHP command", goAuth: false, token: mockapi.ValidAPITokens[0],
			apiURL: stepUpServer.URL, args: []string{"projects"}, message: mfaMessage,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newCommandFactory(t, c.apiURL, authServer.URL)
			if c.goAuth {
				f = newGoAuthCommandFactory(t, c.apiURL, authServer.URL)
			} else {
				f.extraEnv = append(f.extraEnv, EnvPrefix+"GO_AUTH=0")
			}
			f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN="+c.token)
			f.fakeBrowser()

			_, stderr, err := f.RunInteractive("y\n", c.args...)
			assertExitCode(t, 3, err)
			for _, m := range c.message {
				assert.Contains(t, stderr, m)
			}
			assert.NotContains(t, stderr, "Log in via a browser?")
			assert.NotContains(t, stderr, "Cannot log in via the browser")
			assert.NotContains(t, stderr, "Please log in by running")
		})
	}
}

// TestAuthLoginRequired_StoredToken checks that a browser login is still offered with a stored API token, which a
// browser login can replace.
func TestAuthLoginRequired_StoredToken(t *testing.T) {
	t.Parallel()
	for _, goAuth := range []bool{true, false} {
		t.Run(fmt.Sprintf("go auth %v", goAuth), func(t *testing.T) {
			t.Parallel()
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()
			apiHandler := mockapi.NewHandler(t)
			apiHandler.SetMyUser(&mockapi.User{ID: "u1", Username: "testuser", Email: "test@example.com"})
			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			if goAuth {
				f = newGoAuthCommandFactory(t, apiServer.URL, authServer.URL)
			} else {
				f.extraEnv = append(f.extraEnv, EnvPrefix+"GO_AUTH=0")
			}
			f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
			_, stderr, err := f.RunInteractive(mockapi.ValidAPITokens[0]+"\n", "auth:api-token-login")
			require.NoError(t, err, "stderr: %s", stderr)

			apiHandler.RequireStepUp([]string{"mfa"})
			f.fakeBrowser()
			_, stderr, err = f.RunInteractive("n\n", "projects")
			assertExitCode(t, 3, err)
			assert.Contains(t, stderr, "Log in via a browser?")
		})
	}
}

// TestAuthLoginRequired_UnreadableTokenFile checks that an unreadable token file is reported, instead of using a stored
// API token.
func TestAuthLoginRequired_UnreadableTokenFile(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newGoAuthCommandFactory(t, "", authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	f.writeUserTokenFileConfig(filepath.Join(t.TempDir(), "missing"))
	// An API token saved by auth:api-token-login, in the legacy CLI's storage (which is migrated).
	sessDir := filepath.Join(f.home, ".platform-test-cli", ".session", "sess-cli-default")
	require.NoError(t, os.MkdirAll(sessDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sessDir, "api-token"), []byte(mockapi.ValidAPITokens[0]), 0o600))

	_, stderr, err := f.RunCombinedOutput("auth:token", "--no-warn")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "failed to read file")
}
