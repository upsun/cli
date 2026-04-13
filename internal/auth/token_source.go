package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

// sessionTokenSource implements oauth2.TokenSource and the refresher interface
// using the session Manager for persistence.
type sessionTokenSource struct {
	mgr        *session.Manager
	cfg        *config.Config
	httpClient *http.Client
	mu         sync.Mutex
	cached     *oauth2.Token
}

// NewSessionTokenSource creates a token source backed by session files.
//
//nolint:revive // intentionally returns unexported type; callers use := and only call Token/TokenContext
func NewSessionTokenSource(mgr *session.Manager, cfg *config.Config) *sessionTokenSource {
	return &sessionTokenSource{mgr: mgr, cfg: cfg, httpClient: http.DefaultClient}
}

// Token returns a valid access token, refreshing if necessary.
// Implements oauth2.TokenSource. Uses context.Background() for the refresh request;
// use TokenContext for cancellable refresh.
func (ts *sessionTokenSource) Token() (*oauth2.Token, error) {
	return ts.TokenContext(context.Background())
}

// TokenContext is like Token but uses the provided context for any refresh request.
func (ts *sessionTokenSource) TokenContext(ctx context.Context) (*oauth2.Token, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.cached != nil && ts.cached.Valid() {
		return ts.cached, nil
	}

	s, err := ts.mgr.Load()
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if s == nil || s.AccessToken == "" {
		return nil, fmt.Errorf("not logged in: run 'login' to authenticate")
	}

	expiry := time.Unix(s.Expires, 0)
	tok := &oauth2.Token{
		AccessToken:  s.AccessToken,
		TokenType:    s.TokenType,
		RefreshToken: s.RefreshToken,
		Expiry:       expiry,
	}

	if tok.Valid() {
		ts.cached = tok
		return tok, nil
	}

	// Token expired — refresh using the already-loaded session (avoids a second Load).
	if err := ts.unsafeRefreshToken(ctx, s); err != nil {
		return nil, err
	}
	return ts.cached, nil
}

func (ts *sessionTokenSource) unsafeRefreshToken(ctx context.Context, s *session.Session) error {
	ts.cached = nil

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {s.RefreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuth2TokenURL(ts.cfg), strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(ts.cfg.API.OAuth2ClientID, "")

	resp, err := ts.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh token: server returned %d", resp.StatusCode)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("refresh token: decode response: %w", err)
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Unix()
	if result.RefreshToken == "" {
		result.RefreshToken = s.RefreshToken // keep existing if not rotated
	}

	newSession := &session.Session{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		Expires:      expiry,
		RefreshToken: result.RefreshToken,
	}
	if err := ts.mgr.Save(newSession); err != nil {
		return fmt.Errorf("save refreshed session: %w", err)
	}

	ts.cached = &oauth2.Token{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		RefreshToken: result.RefreshToken,
		Expiry:       time.Unix(expiry, 0),
	}
	return nil
}

func (ts *sessionTokenSource) invalidateToken() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.cached = nil
	return nil
}
