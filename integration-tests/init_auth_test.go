package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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

// TestInit_AIRequestIsAuthenticated checks that the AI configuration request is sent with the user's token.
func TestInit_AIRequestIsAuthenticated(t *testing.T) {
	t.Parallel()
	if !goAuthMode() {
		t.Skip("with legacy auth, init needs a JWT access token, and the mock auth server issues opaque ones")
	}
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.Post("/ai/generate-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newInitRepo(t, apiServer.URL, authServer.URL)
	_, stderr, err := f.RunCombinedOutput("init", "--ai=true")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "rate limit exceeded: please try again after 5 seconds")
	assert.Equal(t, "Bearer access-token-1",
		lastRequest(t, apiHandler, "POST", "/ai/generate-configuration").Header.Get("Authorization"))
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
