package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
)

// Transport is an HTTP RoundTripper that adds an access token to requests.
//
// On a 401 response it refreshes the token and retries the request once. A step-up authentication challenge
// (RFC 9470) returns a *LoginRequiredError instead.
type Transport struct {
	Base    http.RoundTripper
	Manager *Manager
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	body, err := bufferBody(req)
	if err != nil {
		return nil, err
	}
	tok, err := t.Manager.Token(ctx, "")
	if err != nil {
		return nil, err
	}
	resp, err := t.base().RoundTrip(withToken(req, tok, body))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	if IsStepUpChallenge(resp) {
		return nil, t.stepUpError(ctx, resp)
	}
	flush(resp.Body)
	tok, err = t.Manager.Token(ctx, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	resp, err = t.base().RoundTrip(withToken(req, tok, body))
	if err == nil && IsStepUpChallenge(resp) {
		return nil, t.stepUpError(ctx, resp)
	}
	return resp, err
}

func (t *Transport) stepUpError(ctx context.Context, resp *http.Response) error {
	defer resp.Body.Close()
	hasAPIToken, _ := t.Manager.HasAPIToken(ctx)
	return StepUpError(resp, hasAPIToken)
}

func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func withToken(req *http.Request, tok *Token, body []byte) *http.Request {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	if body != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	return r
}

// bufferBody reads the request body so that it can be sent twice.
func bufferBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	return b, err
}

func flush(r io.ReadCloser) {
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}

// NewClient returns an HTTP client that authenticates requests.
func NewClient(m *Manager, base http.RoundTripper) *http.Client {
	return &http.Client{Transport: &Transport{Base: base, Manager: m}}
}

// EnsureAuthenticated checks that a token is available, refreshing it if needed.
func (m *Manager) EnsureAuthenticated(ctx context.Context) error {
	_, err := m.Token(ctx, "")
	return err
}

// HasAPIToken reports whether an API token is used, whether stored or set via config.
func (m *Manager) HasAPIToken(ctx context.Context) (bool, error) {
	t, err := m.apiToken(ctx)
	if err != nil {
		return false, err
	}
	return t != "" || m.Settings.AccessToken != "", nil
}

// AsLoginRequired returns a *LoginRequiredError found in err.
func AsLoginRequired(err error) (*LoginRequiredError, bool) {
	var lerr *LoginRequiredError
	ok := errors.As(err, &lerr)
	return lerr, ok
}
