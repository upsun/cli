package tests

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAuthAPITokenLogin_Valid(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", Username: "testuser", Email: "test@example.com"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	// Clear the pre-set TOKEN so we are testing login from scratch.
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",
		EnvPrefix+"NO_INTERACTION=",
		"SHELL_INTERACTIVE=1",
	)
	f.stdin = strings.NewReader(mockapi.ValidAPITokens[0] + "\n")
	_, stderr, err := f.RunCombinedOutput("auth:api-token-login")
	require.NoError(t, err)
	assert.Contains(t, stderr, "logged in")
}

// TestAuthAPITokenLogin_CommandAfterLogin checks that other commands use the stored API token.
func TestAuthAPITokenLogin_CommandAfterLogin(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "u1"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID, Username: "testuser", Email: "test@example.com"})
	apiHandler.SetOrgs([]*mockapi.Org{
		{
			ID:           "org-id-1",
			Name:         "acme",
			Label:        "ACME Inc.",
			Owner:        myUserID,
			Type:         "flexible",
			Capabilities: []string{},
			Links:        mockapi.MakeHALLinks("self=/organizations/" + url.PathEscape("org-id-1")),
		},
	})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	// Clear the pre-set TOKEN so that subsequent commands must rely on the stored one.
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")

	_, stderr, err := f.RunInteractive(mockapi.ValidAPITokens[0]+"\n", "auth:api-token-login")
	require.NoError(t, err, "login must succeed; stderr: %s", stderr)
	assert.Contains(t, stderr, "logged in")

	out, errOut, err := f.RunCombinedOutput("orgs", "--format", "csv", "--columns", "name", "--no-header")
	require.NoError(t, err, "command must succeed after login; stderr: %s", errOut)
	assert.Contains(t, out, "acme")
}

func TestAuthAPITokenLogin_NonInteractive(t *testing.T) {
	f := newCommandFactory(t, "", "")
	_, stderr, err := f.RunCombinedOutput("auth:api-token-login")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Non-interactive use of this command is not supported.")
}

// TestAuthAPITokenLogin_RetryOnInvalid: feeding an invalid token then a valid one
// should succeed on the second attempt (retry up to 5 times on invalid token).
func TestAuthAPITokenLogin_RetryOnInvalid(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", Username: "testuser", Email: "test@example.com"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",
		EnvPrefix+"NO_INTERACTION=",
		"SHELL_INTERACTIVE=1",
	)
	// First line is an invalid token; second is the valid one.
	f.stdin = strings.NewReader("bad-token\n" + mockapi.ValidAPITokens[0] + "\n")

	_, stderr, err := f.RunCombinedOutput("auth:api-token-login")
	require.NoError(t, err, "expected success after retry; stderr: %s", stderr)
	// The auth server's error_description is shown. The CLI's own "Invalid API token" message is
	// unreachable: the OAuth2 provider converts error responses to IdentityProviderException before
	// ApiTokenLoginCommand::exceptionMeansInvalidToken() could see them.
	assert.Contains(t, stderr, "The request could not be authorized.")
	assert.Contains(t, stderr, "The API token is valid.")
}

// TestAuthAPITokenLogin_ExhaustsRetries: 5 consecutive invalid tokens should fail.
func TestAuthAPITokenLogin_ExhaustsRetries(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",
		EnvPrefix+"NO_INTERACTION=",
		"SHELL_INTERACTIVE=1",
	)
	// 5 invalid tokens — all should be rejected, command exits non-zero.
	f.stdin = strings.NewReader("bad1\nbad2\nbad3\nbad4\nbad5\n")

	_, stderr, err := f.RunCombinedOutput("auth:api-token-login")
	require.Error(t, err, "expected failure after 5 invalid tokens")
	assert.Equal(t, 5, strings.Count(stderr, "The request could not be authorized."))
}
