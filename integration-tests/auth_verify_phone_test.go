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

// TestAuthVerifyPhoneNumber_InvalidPhoneRetry: an invalid phone number is re-prompted
// (not an immediate exit), then a valid number succeeds.
func TestAuthVerifyPhoneNumber_InvalidPhoneRetry(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", PhoneNumberVerified: false, Country: "US"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"NO_INTERACTION=0", "SHELL_INTERACTIVE=1")
	// First phone number is invalid; second is valid.
	f.stdin = strings.NewReader("0\nnot-a-number\n+12015550123\n" + mockapi.TestPhoneVerificationCode + "\n")

	_, stderr, err := f.RunCombinedOutput("auth:verify-phone-number")
	require.NoError(t, err, "expected success after retrying with valid number; stderr: %s", stderr)
	assert.Contains(t, stderr, "verified")
}

// TestAuthVerifyPhoneNumber_InvalidCodeRetry: an invalid verification code is re-prompted
// (not an immediate exit), then the correct code succeeds.
func TestAuthVerifyPhoneNumber_InvalidCodeRetry(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", PhoneNumberVerified: false, Country: "US"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"NO_INTERACTION=0", "SHELL_INTERACTIVE=1")
	// Valid phone, then wrong code, then correct code.
	f.stdin = strings.NewReader("0\n+12015550123\n000000\n" + mockapi.TestPhoneVerificationCode + "\n")

	_, stderr, err := f.RunCombinedOutput("auth:verify-phone-number")
	require.NoError(t, err, "expected success after retrying with correct code; stderr: %s", stderr)
	assert.Contains(t, stderr, "verified")
}

// TestAuthVerifyPhoneNumber_ExhaustPhoneAttempts: 5 consecutive invalid phone numbers exit non-zero.
func TestAuthVerifyPhoneNumber_ExhaustPhoneAttempts(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", PhoneNumberVerified: false, Country: "US"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"NO_INTERACTION=0", "SHELL_INTERACTIVE=1")
	f.stdin = strings.NewReader("0\nbad1\nbad2\nbad3\nbad4\nbad5\n")

	_, stderr, err := f.RunCombinedOutput("auth:verify-phone-number")
	require.Error(t, err, "expected failure after 5 invalid phone numbers")
	assert.Contains(t, stderr, "The phone number is not valid.")
}

// TestAuthVerifyPhoneNumber_ExhaustCodeAttempts: 5 consecutive wrong codes exit non-zero.
func TestAuthVerifyPhoneNumber_ExhaustCodeAttempts(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "u1", PhoneNumberVerified: false, Country: "US"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"NO_INTERACTION=0", "SHELL_INTERACTIVE=1")
	f.stdin = strings.NewReader("0\n+12015550123\n000001\n000002\n000003\n000004\n000005\n")

	_, stderr, err := f.RunCombinedOutput("auth:verify-phone-number")
	require.Error(t, err, "expected failure after 5 wrong codes")
	assert.Contains(t, stderr, "Invalid verification code")
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
