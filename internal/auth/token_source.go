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
	mgr    *session.Manager
	cfg    *config.Config
	mu     sync.Mutex
	cached *oauth2.Token
}

// NewSessionTokenSource creates a token source backed by session files.
func NewSessionTokenSource(mgr *session.Manager, cfg *config.Config) *sessionTokenSource {
	return &sessionTokenSource{mgr: mgr, cfg: cfg}
}

// Token returns a valid access token, refreshing if necessary.
func (ts *sessionTokenSource) Token() (*oauth2.Token, error) {
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

	// Token expired — refresh.
	if err := ts.unsafeRefreshToken(); err != nil {
		return nil, err
	}
	return ts.cached, nil
}

func (ts *sessionTokenSource) refreshToken() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.unsafeRefreshToken()
}

func (ts *sessionTokenSource) unsafeRefreshToken() error {
	ts.cached = nil

	s, err := ts.mgr.Load()
	if err != nil || s == nil {
		return fmt.Errorf("session not found for refresh")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {s.RefreshToken},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.cfg.API.OAuth2TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(ts.cfg.API.OAuth2ClientID, "")

	resp, err := http.DefaultClient.Do(req)
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
