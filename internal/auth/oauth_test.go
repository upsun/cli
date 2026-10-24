package auth

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedServer responds to each request with the next status and body, repeating the last one.
type scriptedServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests int
}

type scriptedResponse struct {
	status int
	body   string
}

func newScriptedServer(t *testing.T, responses ...scriptedResponse) *scriptedServer {
	s := &scriptedServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		r := responses[min(s.requests, len(responses)-1)]
		s.requests++
		s.mu.Unlock()
		w.WriteHeader(r.status)
		_, _ = w.Write([]byte(r.body))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *scriptedServer) client() *OAuthClient {
	return &OAuthClient{
		HTTPClient: s.Client(),
		TokenURL:   s.URL + "/token",
		RevokeURL:  s.URL + "/revoke",
		ClientID:   "test",
		retryDelay: time.Millisecond,
	}
}

func TestOAuthClient_Revoke(t *testing.T) {
	cases := []struct {
		name         string
		statuses     []int
		wantErr      string
		wantRequests int
	}{
		{name: "success", statuses: []int{200}, wantRequests: 1},
		{name: "retry status, then success", statuses: []int{503, 200}, wantRequests: 2},
		{name: "retry status twice", statuses: []int{429, 502}, wantErr: "status 502", wantRequests: 2},
		{name: "client error", statuses: []int{400}, wantErr: "status 400", wantRequests: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var responses []scriptedResponse
			for _, s := range c.statuses {
				responses = append(responses, scriptedResponse{status: s})
			}
			srv := newScriptedServer(t, responses...)
			err := srv.client().Revoke(context.Background(), "token", "access_token")
			if c.wantErr != "" {
				assert.ErrorContains(t, err, c.wantErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, c.wantRequests, srv.requests)
		})
	}
}

func TestOAuthClient_TokenResponses(t *testing.T) {
	cases := []struct {
		name         string
		responses    []scriptedResponse
		wantToken    string
		wantErr      string
		wantCode     string
		wantRequests int
	}{
		{
			name:         "valid",
			responses:    []scriptedResponse{{200, `{"access_token": "at", "expires_in": 60}`}},
			wantToken:    "at",
			wantRequests: 1,
		},
		{
			name:         "invalid JSON",
			responses:    []scriptedResponse{{200, `not json`}},
			wantErr:      "invalid token response",
			wantRequests: 2, // Retried, as the exchange can be repeated.
		},
		{
			name:         "an error with a success status",
			responses:    []scriptedResponse{{200, `{"error": "access_denied"}`}},
			wantCode:     "access_denied",
			wantRequests: 1,
		},
		{
			name:         "no access token",
			responses:    []scriptedResponse{{200, `{}`}},
			wantErr:      "no access token",
			wantRequests: 2, // Retried, as the exchange can be repeated.
		},
		{
			name: "a server error is retried once",
			responses: []scriptedResponse{
				{503, `{"error": "server_error"}`},
				{200, `{"access_token": "at"}`},
			},
			wantToken:    "at",
			wantRequests: 2,
		},
		{
			name:         "repeated server errors",
			responses:    []scriptedResponse{{503, `{"error": "server_error"}`}},
			wantCode:     "server_error",
			wantRequests: 2,
		},
		{
			name:         "a client error is not retried",
			responses:    []scriptedResponse{{400, `{"error": "request_unauthorized"}`}},
			wantCode:     "request_unauthorized",
			wantRequests: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newScriptedServer(t, c.responses...)
			e, err := srv.client().ExchangeAPIToken(context.Background(), "api-token")
			switch {
			case c.wantCode != "":
				var oerr *OAuthError
				require.ErrorAs(t, err, &oerr)
				assert.Equal(t, c.wantCode, oerr.Code)
			case c.wantErr != "":
				assert.ErrorContains(t, err, c.wantErr)
			default:
				require.NoError(t, err)
				assert.Equal(t, c.wantToken, e.AccessToken)
			}
			assert.Equal(t, c.wantRequests, srv.requests)
		})
	}
}

// TestOAuthClient_Unreachable checks that requests that were not sent are retried twice.
func TestOAuthClient_Unreachable(t *testing.T) {
	srv := newScriptedServer(t, scriptedResponse{200, `{}`})
	c := srv.client()
	srv.Close()
	_, err := c.Refresh(context.Background(), "rt")
	assert.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Refresh(ctx, "rt")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestUnsafeGetJWTExpiry(t *testing.T) {
	encode := func(payload string) string {
		return "header." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
	}
	cases := []struct {
		name    string
		token   string
		want    int64
		wantErr string
	}{
		{name: "valid", token: encode(`{"exp": 1700000000}`), want: 1700000000},
		{name: "empty", token: "", wantErr: "empty token"},
		{name: "not a JWT", token: "opaque", wantErr: "malformed token"},
		{name: "invalid base64", token: "header.!!!.signature", wantErr: "decode payload"},
		{name: "invalid JSON", token: encode(`not json`), wantErr: "unmarshal claims"},
		{name: "no expiry", token: encode(`{"sub": "u1"}`), wantErr: "exp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exp, err := unsafeGetJWTExpiry(c.token)
			if c.wantErr != "" {
				assert.ErrorContains(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, exp.Unix())
		})
	}
}

func TestLoginRequiredAfterRefreshError(t *testing.T) {
	cases := []struct {
		description string
		wantNotice  string
		wantAPI     bool
	}{
		{"The refresh token is invalid.", "Your session has expired. You have been logged out.", false},
		{"Your SSO session has expired.", "Your SSO session has expired. You have been logged out.", false},
		{"The API token has been revoked.", "The API token is invalid.", true},
	}
	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			lerr := loginRequiredAfterRefreshError(&OAuthError{Code: "invalid_grant", Description: c.description})
			assert.Equal(t, c.wantNotice, lerr.Notice)
			assert.Equal(t, c.wantAPI, lerr.HasAPIToken)
		})
	}
}

func TestLoginRequiredError_Message(t *testing.T) {
	maxAge := 60
	cases := []struct {
		name string
		err  LoginRequiredError
		want string
	}{
		{name: "no challenge", want: "Authentication is required."},
		{
			name: "MFA",
			err:  LoginRequiredError{AuthMethods: []string{"mfa"}},
			want: "Multi-factor authentication (MFA) is required.",
		},
		{name: "SSO", err: LoginRequiredError{AuthMethods: []string{"sso:acme"}}, want: "Single sign-on (SSO) is required."},
		{name: "max age", err: LoginRequiredError{MaxAge: &maxAge}, want: "More recent authentication is required."},
		{
			name: "other methods",
			err:  LoginRequiredError{AuthMethods: []string{"pwd", "mfa"}},
			want: "Re-authentication is required.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.err.Error())
		})
	}
}
