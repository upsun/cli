package tests

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAuthInfo(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{
		ID:                  "my-user-id",
		Deactivated:         false,
		Namespace:           "ns",
		Username:            "my-username",
		FirstName:           "Foo",
		LastName:            "Bar",
		Email:               "my-user@example.com",
		EmailVerified:       true,
		Picture:             "https://example.com/profile.png",
		Country:             "NO",
		PhoneNumberVerified: true,
		MFAEnabled:          true,
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assertTrimmed(t, `
+-----------------------+---------------------+
| Property              | Value               |
+-----------------------+---------------------+
| id                    | my-user-id          |
| first_name            | Foo                 |
| last_name             | Bar                 |
| username              | my-username         |
| email                 | my-user@example.com |
| phone_number_verified | true                |
+-----------------------+---------------------+
`, f.Run("auth:info", "-v", "--refresh"))

	assert.Equal(t, "my-user-id\n", f.Run("auth:info", "-P", "id"))
}

func TestAuthInfo_NoAutoLogin_NotLoggedIn(t *testing.T) {
	f := newCommandFactory(t, "", "")
	// No auth configured — with --no-auto-login should exit 0 and produce no stdout.
	out, stderr, err := f.RunCombinedOutput("auth:info", "--no-auto-login", "-P", "id")
	t.Log("stderr:", stderr)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(out))
}

func TestAuthInfo_NotLoggedIn_DeclineRelogin(t *testing.T) {
	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"NO_INTERACTION=", // allow interactive mode (testEnv sets it to 1)
		"SHELL_INTERACTIVE=1",
	)
	f.stdin = strings.NewReader("n\n")

	_, stderr, err := f.RunCombinedOutput("auth:info")
	require.Error(t, err, "expected exit 1 when user declines re-login")
	assert.Contains(t, stderr, "Authentication is required.")
	assert.Contains(t, stderr, "not logged in")
}

func TestAuthInfo_ExpiredSession_DeclineRelogin(t *testing.T) {
	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"NO_INTERACTION=", // allow interactive mode (testEnv sets it to 1)
		"SHELL_INTERACTIVE=1",
	)

	// Pre-populate an expired OAuth session.
	writeOAuthSession(t, f.homeDir, "default", map[string]interface{}{
		"accessToken":  "expired-token",
		"tokenType":    "bearer",
		"expires":      time.Now().Add(-time.Hour).Unix(),
		"refreshToken": "expired-refresh",
	})

	f.stdin = strings.NewReader("n\n")

	_, stderr, err := f.RunCombinedOutput("auth:info")
	require.Error(t, err, "expected exit 1 when user declines re-login after session expiry")
	assert.Contains(t, stderr, "Your session has expired. You have been logged out.")
	assert.Contains(t, stderr, "Authentication is required.")
	assert.Contains(t, stderr, "not logged in")
}

func TestAuthInfo_NotLoggedIn_NoInteraction(t *testing.T) {
	// testEnv sets NO_INTERACTION=1 via env var — no prompt should appear.
	f := newCommandFactory(t, "", "")

	_, stderr, err := f.RunCombinedOutput("auth:info")
	require.Error(t, err, "expected exit 1 when not logged in (non-interactive via env)")
	assert.NotContains(t, stderr, "Log in via a browser")
	assert.Contains(t, stderr, "not logged in")
}

func TestAuthInfo_NotLoggedIn_FlagNoInteraction(t *testing.T) {
	// --no-interaction flag (via Viper) must also suppress the prompt.
	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv, EnvPrefix+"NO_INTERACTION=")

	_, stderr, err := f.RunCombinedOutput("auth:info", "--no-interaction")
	require.Error(t, err, "expected exit 1 when not logged in (--no-interaction flag)")
	assert.NotContains(t, stderr, "Log in via a browser")
	assert.Contains(t, stderr, "not logged in")
}

func TestAuthInfo_NotLoggedIn_FlagYes(t *testing.T) {
	// --yes must auto-accept the browser login prompt and complete the full flow.
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", Username: "testuser", Email: "test@example.com"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",       // clear API token so browser flow is used
		EnvPrefix+"NO_INTERACTION=", // clear env var; --yes must override
		"SHELL_INTERACTIVE=1",
	)

	cmd := f.buildCommand("auth:info", "--yes")
	cmd.Stderr = nil
	stderrPipe, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	// Read stderr until we find the local callback server port.
	portCh := make(chan string, 1)
	go func() {
		re := regexp.MustCompile(`127\.0\.0\.1:(\d+)`)
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			if m := re.FindStringSubmatch(scanner.Text()); m != nil {
				portCh <- m[1]
				for scanner.Scan() { // drain
				}
				return
			}
		}
	}()

	select {
	case port := <-portCh:
		localURL := fmt.Sprintf("http://127.0.0.1:%s", port)
		time.Sleep(100 * time.Millisecond)

		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
		resp, err := noRedirect.Get(localURL)
		require.NoError(t, err)
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()

		authResp, err := noRedirect.Get(loc)
		require.NoError(t, err)
		callbackLoc := authResp.Header.Get("Location")
		_ = authResp.Body.Close()

		callbackResp, err := http.Get(callbackLoc) //nolint:noctx
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, callbackResp.Body)
		_ = callbackResp.Body.Close()

	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for browser callback server to start")
	}

	require.NoError(t, cmd.Wait())
}

func TestAuthInfo_DeprecatedAliases(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{
		ID: "uid-1", FirstName: "Foo", LastName: "Bar", Email: "foo@example.com",
	})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()
	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// display_name is deprecated but must still work.
	out := f.Run("auth:info", "-P", "display_name")
	assert.Equal(t, "Foo Bar\n", out)

	// mail is deprecated alias for email.
	out = f.Run("auth:info", "-P", "mail")
	assert.Equal(t, "foo@example.com\n", out)
}
