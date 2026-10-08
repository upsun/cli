package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ExitCodeLoginRequired is the exit code used when login is required.
const ExitCodeLoginRequired = 3

// LoginRequiredError means the user must log in (again).
type LoginRequiredError struct {
	// Notice is shown before the login prompt, e.g. "Your session has expired. You have been logged out."
	Notice string `json:"notice,omitempty"`

	// AuthMethods and MaxAge come from a step-up authentication challenge (RFC 9470).
	AuthMethods []string `json:"amr,omitempty"`
	MaxAge      *int     `json:"max_age,omitempty"`

	HasAPIToken bool `json:"has_api_token,omitempty"`
}

func (e *LoginRequiredError) Error() string {
	return e.Message()
}

// Message returns a short description, matching the legacy CLI's LoginRequiredEvent::getMessage().
func (e *LoginRequiredError) Message() string {
	msg := "Authentication is required."
	if len(e.AuthMethods) > 0 || e.MaxAge != nil {
		msg = "Re-authentication is required."
	}
	switch {
	case len(e.AuthMethods) == 1 && e.AuthMethods[0] == "mfa":
		msg = "Multi-factor authentication (MFA) is required."
	case len(e.AuthMethods) == 1 && strings.HasPrefix(e.AuthMethods[0], "sso:"):
		msg = "Single sign-on (SSO) is required."
	case len(e.AuthMethods) != 1 && e.MaxAge != nil:
		msg = "More recent authentication is required."
	}
	return msg
}

// loginRequiredAfterRefreshError converts a failed refresh into a login-required error, with the legacy CLI's notices.
func loginRequiredAfterRefreshError(oerr *OAuthError) *LoginRequiredError {
	switch {
	case strings.Contains(oerr.Description, "SSO session has expired"):
		return &LoginRequiredError{Notice: "Your SSO session has expired. You have been logged out."}
	case strings.Contains(oerr.Description, "API token"):
		return &LoginRequiredError{Notice: "The API token is invalid.", HasAPIToken: true}
	default:
		return &LoginRequiredError{Notice: "Your session has expired. You have been logged out."}
	}
}

// isInvalidAPITokenError checks if a token endpoint error means the API token was rejected.
func isInvalidAPITokenError(oerr *OAuthError) bool {
	if oerr.StatusCode != http.StatusBadRequest && oerr.StatusCode != http.StatusUnauthorized {
		return false
	}
	return oerr.Code == "invalid_grant" || oerr.Code == "request_unauthorized"
}

// IsStepUpChallenge checks for a step-up authentication response (RFC 9470).
func IsStepUpChallenge(resp *http.Response) bool {
	if resp.StatusCode != http.StatusUnauthorized {
		return false
	}
	h := strings.Join(resp.Header.Values("WWW-Authenticate"), "\n")
	return strings.Contains(strings.ToLower(h), "bearer") && strings.Contains(h, "insufficient_user_authentication")
}

// StepUpError reads the required authentication methods and max age from a step-up response body.
func StepUpError(resp *http.Response, hasAPIToken bool) *LoginRequiredError {
	var body struct {
		AMR    []string `json:"amr"`
		MaxAge *int     `json:"max_age"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(b, &body)
	return &LoginRequiredError{AuthMethods: body.AMR, MaxAge: body.MaxAge, HasAPIToken: hasAPIToken}
}
