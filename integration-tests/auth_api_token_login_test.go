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

// TestAuthAPITokenLogin_PHPCommandAfterLogin verifies that after logging in via
// the Go auth:api-token-login command, subsequent PHP commands can authenticate
// using the stored session (via injectSessionAuth), without TOKEN being pre-set.
// This is a regression test for the credential-helper incompatibility bug.
func TestAuthAPITokenLogin_PHPCommandAfterLogin(t *testing.T) {
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
	// Clear the pre-set TOKEN so that subsequent commands must rely on the stored session.
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",
		EnvPrefix+"AUTH_URL="+authServer.URL,
		"SHELL_INTERACTIVE=1",
	)

	// Step 1: Log in via the Go auth:api-token-login command.
	// Token is passed as an argument to avoid interactive stdin complications.
	_, stderr, err := f.RunCombinedOutput("auth:api-token-login", mockapi.ValidAPITokens[0])
	require.NoError(t, err, "login must succeed; stderr: %s", stderr)
	assert.Contains(t, stderr, "logged in")

	// Step 2: Run a PHP-backed command (orgs) without TOKEN in env.
	// injectSessionAuth must read the stored API token and inject it into the PHP subprocess.
	out, errOut, err := f.RunCombinedOutput("orgs", "--format", "csv", "--columns", "name", "--no-header")
	require.NoError(t, err, "php command must succeed after login; stderr: %s", errOut)
	assert.Contains(t, out, "acme")
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
