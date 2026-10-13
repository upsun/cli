package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"

	"github.com/upsun/cli/internal/auth/store"
)

// OAuthError is an OAuth 2.0 error response (RFC 6749, section 5.2).
type OAuthError struct {
	StatusCode  int    `json:"-"`
	Code        string `json:"error"`
	Description string `json:"error_description"`
	Hint        string `json:"error_hint"`
}

func (e *OAuthError) Error() string {
	if e.Description != "" {
		return e.Description
	}
	if e.Code != "" {
		return e.Code
	}
	return fmt.Sprintf("OAuth 2.0 request failed with status %d", e.StatusCode)
}

// tokenResponse is a successful token endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// OAuthClient calls the OAuth 2.0 token and revocation endpoints, as a public client.
type OAuthClient struct {
	HTTPClient *http.Client
	TokenURL   string
	RevokeURL  string
	ClientID   string

	// retryDelay is the base delay between retries, for tests.
	retryDelay time.Duration
}

// requestTimeout bounds each request to the auth server.
const requestTimeout = 30 * time.Second

// ExchangeCode exchanges an authorization code (with its PKCE verifier) for tokens.
func (c *OAuthClient) ExchangeCode(ctx context.Context, code, verifier, redirectURI string) (*store.Entry, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	return c.postToken(ctx, form, true)
}

// ExchangeAPIToken exchanges an API token for tokens, using the "api_token" grant.
func (c *OAuthClient) ExchangeAPIToken(ctx context.Context, apiToken string) (*store.Entry, error) {
	return c.withRetries(ctx, 1, func(ctx context.Context) (*store.Entry, bool, error) {
		return c.postTokenTraced(ctx, c.clientForm(url.Values{"grant_type": {"api_token"}, "api_token": {apiToken}}))
	})
}

// Refresh uses a refresh token to get new tokens.
//
// Connection errors before the request is sent are retried twice. Errors after it is sent are not retried, because
// the server may have consumed the refresh token.
func (c *OAuthClient) Refresh(ctx context.Context, refreshToken string) (*store.Entry, error) {
	return c.withRetries(ctx, 0, func(ctx context.Context) (*store.Entry, bool, error) {
		form := c.clientForm(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
		return c.postTokenTraced(ctx, form)
	})
}

// Revoke revokes a token. The hint is "access_token" or "refresh_token".
func (c *OAuthClient) Revoke(ctx context.Context, token, hint string) error {
	form := c.clientForm(url.Values{"token": {token}, "token_type_hint": {hint}})
	var err error
	for attempt := range 2 {
		var status int
		status, _, err = c.post(ctx, c.RevokeURL, form, false)
		if err != nil {
			return err
		}
		if status < 300 {
			return nil
		}
		err = fmt.Errorf("token revocation failed with status %d", status)
		// Retry once on a retry status, as the legacy CLI does.
		switch status {
		case 408, 429, 502, 503, 504:
			if attempt == 0 {
				continue
			}
		}
		return err
	}
	return err
}

func (c *OAuthClient) clientForm(form url.Values) url.Values {
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", "")
	return form
}

// withRetries runs a token request, retrying transient failures. The callback reports whether the request was sent.
// Transient failures after the request was sent are retried up to sentRetries times.
func (c *OAuthClient) withRetries(
	ctx context.Context,
	sentRetries int,
	fn func(ctx context.Context) (*store.Entry, bool, error),
) (*store.Entry, error) {
	delay := c.retryDelay
	if delay == 0 {
		delay = 500 * time.Millisecond
	}
	unsentRetries := 2
	for {
		e, sent, err := fn(ctx)
		if err == nil || ctx.Err() != nil {
			return e, err
		}
		var oerr *OAuthError
		isOAuthError := errors.As(err, &oerr)
		switch {
		case !sent && unsentRetries > 0:
			unsentRetries--
		case sent && sentRetries > 0 && (!isOAuthError || oerr.StatusCode >= 500):
			sentRetries--
		default:
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
}

// postTokenTraced posts to the token endpoint, and reports whether the request was written.
func (c *OAuthClient) postTokenTraced(ctx context.Context, form url.Values) (*store.Entry, bool, error) {
	var sent bool
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { sent = true }}
	e, err := c.postToken(httptrace.WithClientTrace(ctx, trace), form, false)
	var oerr *OAuthError
	if errors.As(err, &oerr) {
		// A response was received.
		sent = true
	}
	return e, sent, err
}

func (c *OAuthClient) postToken(ctx context.Context, form url.Values, basicAuth bool) (*store.Entry, error) {
	status, body, err := c.post(ctx, c.TokenURL, form, basicAuth)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		oerr := &OAuthError{StatusCode: status}
		_ = json.Unmarshal(body, oerr)
		return nil, oerr
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("invalid token response: %w", err)
	}
	if tr.AccessToken == "" {
		oerr := &OAuthError{StatusCode: status}
		if json.Unmarshal(body, oerr) == nil && oerr.Code != "" {
			return nil, oerr
		}
		return nil, errors.New("invalid token response: no access token")
	}
	e := &store.Entry{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
	}
	if tr.ExpiresIn > 0 {
		e.Expires = time.Now().Unix() + tr.ExpiresIn
	} else if exp, err := unsafeGetJWTExpiry(tr.AccessToken); err == nil {
		e.Expires = exp.Unix()
	}
	return e, nil
}

// post sends a form, and returns the response status and body. Each request is bounded by requestTimeout.
func (c *OAuthClient) post(
	ctx context.Context, u string, form url.Values, basicAuth bool,
) (status int, body []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basicAuth {
		req.SetBasicAuth(c.ClientID, "")
	}
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, err
}
