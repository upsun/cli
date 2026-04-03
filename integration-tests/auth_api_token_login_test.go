package tests

import (
	"net/http/httptest"
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
	// Clear NO_INTERACTION so the PHP CLI can run interactively.
	// Set TEST_CLI_AUTH_URL so the PHP CLI can reach the mock auth server.
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",
		EnvPrefix+"NO_INTERACTION=",
		EnvPrefix+"AUTH_URL="+authServer.URL,
		"SHELL_INTERACTIVE=1",
	)
	// Pipe stdin: first the API token, then "n" to reject any browser login prompt
	// that the PHP CLI may trigger when initializing the API client.
	f.stdin = strings.NewReader(mockapi.ValidAPITokens[0] + "\nn\n" + mockapi.ValidAPITokens[0] + "\n")
	_, stderr, err := f.RunCombinedOutput("auth:api-token-login")
	require.NoError(t, err)
	assert.Contains(t, stderr, "logged in")
}

func TestAuthAPITokenLogin_Invalid(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")
	_, _, err := f.RunCombinedOutput("auth:api-token-login", "bad-token")
	assert.Error(t, err)
}
