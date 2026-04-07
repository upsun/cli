// integration-tests/auth_browser_login_test.go
package tests

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAuthBrowserLogin_Success(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", Username: "testuser", Email: "test@example.com"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	// Clear API token so the command doesn't bail out with "Cannot log in via the browser".
	// Clear NO_INTERACTION so the command doesn't bail out with "Non-interactive use of this command is not supported".
	// Set SHELL_INTERACTIVE so the PHP CLI treats stdin as interactive even without a real TTY.
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",
		EnvPrefix+"NO_INTERACTION=",
		"SHELL_INTERACTIVE=1",
	)

	cmd := f.buildCommand("auth:browser-login")
	// Override any stderr set by buildCommand (e.g. in verbose mode) so we can pipe it.
	cmd.Stderr = nil
	stderrPipe, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	// Read stderr lines until we find the local server port.
	portCh := make(chan string, 1)
	go func() {
		re := regexp.MustCompile(`127\.0\.0\.1:(\d+)`)
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			if m := re.FindStringSubmatch(line); m != nil {
				portCh <- m[1]
				// Drain remaining stderr.
				for scanner.Scan() {
				}
				return
			}
		}
	}()

	select {
	case port := <-portCh:
		// Simulate the browser hitting the callback with a valid auth code.
		// The CLI's local server is at 127.0.0.1:<port>.
		// We need to get the state parameter first by following the authorize redirect.
		// Use a non-redirecting client to capture the state.
		localURL := fmt.Sprintf("http://127.0.0.1:%s", port)

		// Give the local server a moment to start.
		time.Sleep(100 * time.Millisecond)

		// Fetch the local page to get the redirect URL with state.
		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
		resp, err := noRedirect.Get(localURL)
		require.NoError(t, err)
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()

		// Extract state from the authorize redirect.
		stateRe := regexp.MustCompile(`[?&]state=([^&]+)`)
		stateMatch := stateRe.FindStringSubmatch(loc)
		require.NotEmpty(t, stateMatch, "expected state in redirect URL, got: %s", loc)

		// Hit the authorize endpoint to get the auth code.
		authResp, err := noRedirect.Get(loc)
		require.NoError(t, err)
		callbackLoc := authResp.Header.Get("Location")
		_ = authResp.Body.Close()

		// The auth server redirects to our local callback — GET to simulate browser.
		callbackResp, err := http.Get(callbackLoc)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, callbackResp.Body)
		_ = callbackResp.Body.Close()

	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for local server to start")
	}

	err = cmd.Wait()
	require.NoError(t, err)
}

// writeOAuthSession writes a pre-populated OAuth session directly to the filesystem for a given
// homeDir and session ID. This bypasses the session.Manager so integration tests can set up
// authenticated state without running a full login flow.
func writeOAuthSession(t *testing.T, homeDir, sessionID string, s map[string]interface{}) {
	t.Helper()
	base := filepath.Join(homeDir, ".platform-test-cli", ".session")
	sessDir := filepath.Join(base, "sess-"+sessionID)
	cliDir := filepath.Join(base, "sess-cli-"+sessionID)
	require.NoError(t, os.MkdirAll(sessDir, 0700))
	require.NoError(t, os.MkdirAll(cliDir, 0700))
	data, err := json.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(sessDir, "sess-"+sessionID+".json"), data, 0600))
}

func TestAuthBrowserLogin_AlreadyLoggedInOAuth_DeclineRelogin(t *testing.T) {
	// No servers needed: the command exits before reaching the browser flow.
	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"NO_INTERACTION=", // allow interactive mode (testEnv sets it to 1)
		"SHELL_INTERACTIVE=1",
	)

	// Pre-populate a valid, non-expired OAuth session.
	writeOAuthSession(t, f.homeDir, "default", map[string]interface{}{
		"accessToken":  "test-oauth-token",
		"tokenType":    "bearer",
		"expires":      time.Now().Add(time.Hour).Unix(),
		"refreshToken": "test-refresh",
	})

	// Pipe "n" to decline re-login.
	f.stdin = strings.NewReader("n\n")

	_, stderr, err := f.RunCombinedOutput("auth:browser-login")
	require.NoError(t, err, "expected exit 0 when user declines re-login, stderr: %s", stderr)
	assert.Contains(t, stderr, "You are already logged in")
}

func TestAuthBrowserLogin_AlreadyLoggedIn(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", Username: "testuser", Email: "test@example.com"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	// TOKEN is set by the factory → hasApiToken returns true → command refuses to start browser login.
	_, stderr, err := f.RunCombinedOutput("auth:browser-login")
	// The command exits non-zero when an API token is configured.
	require.Error(t, err)
	assert.True(t,
		strings.Contains(stderr, "Cannot log in via the browser") || strings.Contains(stderr, "log in"),
		"unexpected stderr: %s", stderr,
	)
}
