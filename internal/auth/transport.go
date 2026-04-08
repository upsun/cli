package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

type refresher interface {
	invalidateToken() error
}

// Transport is an HTTP RoundTripper similar to golang.org/x/oauth2.Transport.
// It injects Authorization headers using a savingSource and, on a 401 response,
// clears the cached token and retries the request once.
type Transport struct {
	// base is the underlying oauth2.Transport that adds the Authorization header.
	base http.RoundTripper

	// refresher is the savingSource used as the TokenSource for base; kept private
	// so we can clear its cached token on 401.
	refresher refresher

	LogFunc func(msg string, args ...any)
}

// RoundTrip adds Authorization via the underlying oauth2.Transport. If the
// response is 401 Unauthorized, it clears the cached token and retries once.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	wrapRequest(req)

	resp, err := t.base.RoundTrip(req)

	// Retry on 401
	if resp != nil && resp.StatusCode == http.StatusUnauthorized {
		_ = t.log("The access token needs to be refreshed. Retrying request.")
		if err := t.refresher.invalidateToken(); err != nil {
			return nil, fmt.Errorf("failed to invalidate token: %w", err)
		}
		flushReader(resp.Body)
		if req.GetBody != nil {
			req.Body, err = req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("failed to rewind request body: %w", err)
			}
		}
		resp, err = t.base.RoundTrip(req)
	}

	return resp, err
}

func (t *Transport) log(msg string, args ...any) error {
	if t.LogFunc == nil {
		return nil
	}
	t.LogFunc(msg, args...)
	return nil
}

// context key for storing a custom RoundTripper.
type transportCtxKey struct{}

// WithTransport returns a new context that carries the provided RoundTripper.
func WithTransport(ctx context.Context, rt http.RoundTripper) context.Context {
	return context.WithValue(ctx, transportCtxKey{}, rt)
}

// TransportFromContext retrieves a RoundTripper previously stored with
// WithTransport. It returns (nil, false) if none is set.
func TransportFromContext(ctx context.Context) (http.RoundTripper, bool) {
	v := ctx.Value(transportCtxKey{})
	if v == nil {
		return nil, false
	}
	rt, ok := v.(http.RoundTripper)
	if !ok || rt == nil {
		return nil, false
	}
	return rt, true
}

// wrapRequest buffers req.Body so that it can be replayed on retry.
// It stores the bytes in req.GetBody so RoundTrip can restore the body
// before the second attempt (bytes.Buffer is drained after the first read).
func wrapRequest(req *http.Request) {
	if req.Body == nil {
		return
	}
	b, _ := io.ReadAll(req.Body)
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(b))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
}

func flushReader(r io.ReadCloser) {
	if r == nil {
		return
	}
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}
