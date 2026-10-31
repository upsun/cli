package tests

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAuthInfo(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	f := newCommandFactory(t, "", "")
	// No auth configured — with --no-auto-login should exit 0 and produce no stdout.
	out, stderr, err := f.RunCombinedOutput("auth:info", "--no-auto-login", "-P", "id")
	t.Log("stderr:", stderr)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(out))
}

func TestAuthInfo_NotLoggedIn_DeclineRelogin(t *testing.T) {
	t.Parallel()
	f := newCommandFactory(t, "", "")
	f.fakeBrowser()
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"NO_INTERACTION=", // allow interactive mode (testEnv sets it to 1)
		"SHELL_INTERACTIVE=1",
	)
	f.stdin = strings.NewReader("n\n")

	_, stderr, err := f.RunCombinedOutput("auth:info")
	assertExitCode(t, 3, err)
	assert.Contains(t, stderr, "Log in via a browser?")
	assert.Contains(t, stderr, "Please log in by running:")
}

func TestAuthInfo_ExpiredSession_DeclineRelogin(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	f := newCommandFactory(t, "", authServer.URL)
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TOKEN=",
		EnvPrefix+"NO_INTERACTION=", // allow interactive mode (testEnv sets it to 1)
		"SHELL_INTERACTIVE=1",
	)

	// Pre-populate an expired OAuth session.
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken":  "expired-token",
		"tokenType":    "bearer",
		"expires":      time.Now().Add(-time.Hour).Unix(),
		"refreshToken": "expired-refresh",
	})

	f.stdin = strings.NewReader("n\n")

	_, stderr, err := f.RunCombinedOutput("auth:info")
	assertExitCode(t, 3, err)
	assert.Contains(t, stderr, "Your session has expired. You have been logged out.")
	assert.Contains(t, stderr, "Please log in by running:")
}

func TestAuthInfo_NotLoggedIn_NoInteraction(t *testing.T) {
	t.Parallel()
	// testEnv sets NO_INTERACTION=1 via env var — no prompt should appear.
	f := newCommandFactory(t, "", "")

	_, stderr, err := f.RunCombinedOutput("auth:info")
	assertExitCode(t, 3, err)
	assert.NotContains(t, stderr, "Log in via a browser")
	assert.Contains(t, stderr, "Please log in by running:")
}

func TestAuthInfo_NotLoggedIn_FlagNoInteraction(t *testing.T) {
	t.Parallel()
	// The --no-interaction flag must also suppress the prompt.
	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv, EnvPrefix+"NO_INTERACTION=")

	_, stderr, err := f.RunCombinedOutput("auth:info", "--no-interaction")
	assertExitCode(t, 3, err)
	assert.NotContains(t, stderr, "Log in via a browser")
	assert.Contains(t, stderr, "Please log in by running:")
}

func TestAuthInfo_NotLoggedIn_FlagYes(t *testing.T) {
	t.Parallel()
	// --yes implies non-interactive use, so no login prompt is shown.
	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv, EnvPrefix+"NO_INTERACTION=", "SHELL_INTERACTIVE=1")

	_, stderr, err := f.RunCombinedOutput("auth:info", "--yes")
	assertExitCode(t, 3, err)
	assert.NotContains(t, stderr, "Log in via a browser")
	assert.Contains(t, stderr, "Please log in by running:")
}

func TestAuthInfo_DeprecatedAliases(t *testing.T) {
	t.Parallel()
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

	stdout, stderr, err := f.RunCombinedOutput("auth:info", "-P", "uuid")
	require.NoError(t, err)
	assert.Equal(t, "uid-1\n", stdout)
	assert.Contains(t, stderr, `the "uuid" property is now named "id".`)

	_, stderr, err = f.RunCombinedOutput("auth:info", "-P", "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, stderr, "Property not found: nonexistent")

	_, stderr, err = f.RunCombinedOutput("auth:info", "email", "-P", "id")
	assert.Error(t, err)
	assert.Contains(t, stderr, "You cannot use both the <property> argument and the --property option")
}

// TestAuthInfo_SessionID checks that a session switched with session:switch is shown.
func TestAuthInfo_SessionID(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "uid-1"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()
	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	f.Run("session:switch", "work")
	_, stderr, err := f.RunCombinedOutput("auth:info")
	require.NoError(t, err)
	assert.Contains(t, stderr, "The current session ID is: work\nChange this using: platform-test session:switch")
}
