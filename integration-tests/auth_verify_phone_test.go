// integration-tests/auth_verify_phone_test.go
package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestAuthVerifyPhoneNumber_AlreadyVerified(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", PhoneNumberVerified: true})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	// The PHP command checks isInteractive() before checking phone status, so we must
	// disable NO_INTERACTION. SHELL_INTERACTIVE makes the PHP CLI treat stdin as a TTY.
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"NO_INTERACTION=0",
		"SHELL_INTERACTIVE=1",
	)
	_, stderr, err := f.RunCombinedOutput("auth:verify-phone-number")
	require.NoError(t, err)
	assert.Contains(t, stderr, "already has a verified phone number")
}

func TestAuthVerifyPhoneNumber_SMSFlow(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", PhoneNumberVerified: false, Country: "US"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	// Disable NO_INTERACTION so the command accepts interactive prompts.
	// Set SHELL_INTERACTIVE so the PHP CLI treats stdin as interactive even without a real TTY.
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"NO_INTERACTION=0",
		"SHELL_INTERACTIVE=1",
	)
	// Use the factory stdin field so buildCommand wires it up before CombinedOutput locks stderr.
	// Provide stdin: method=sms (choice 0), phone number, verification code.
	// The number must be a valid E.164 number parseable with the user's country (US).
	f.stdin = strings.NewReader("0\n+12015550123\n" + mockapi.TestPhoneVerificationCode + "\n")
	_, stderr, err := f.RunCombinedOutput("auth:verify-phone-number")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "verified")
}
