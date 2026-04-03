package auth

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

// Client is an authenticated HTTP client for the Upsun API.
type Client struct {
	HTTPClient  *http.Client
	tokenSource *sessionTokenSource
}

// EnsureAuthenticated checks that a valid token is available.
func (c *Client) EnsureAuthenticated(_ context.Context) error {
	_, err := c.tokenSource.Token()
	return err
}

// NewClient creates an HTTP client authenticated via the session Manager.
func NewClient(ctx context.Context, mgr *session.Manager, cfg *config.Config) (*Client, error) {
	ts := NewSessionTokenSource(mgr, cfg)

	baseRT := http.DefaultTransport
	if rt, ok := TransportFromContext(ctx); ok && rt != nil {
		baseRT = rt
	}

	httpClient := &http.Client{
		Transport: &Transport{
			refresher: ts,
			base: &oauth2.Transport{
				Source: ts,
				Base:   baseRT,
			},
		},
	}

	return &Client{
		HTTPClient:  httpClient,
		tokenSource: ts,
	}, nil
}
