package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

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
