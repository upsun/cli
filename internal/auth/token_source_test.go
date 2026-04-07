package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

func TestSessionTokenSource_ValidToken(t *testing.T) {
	cfg := loadTestConfig(t)
	store := session.NewMemStore()
	mgr := session.NewWithStore(cfg, store)

	future := time.Now().Add(time.Hour).Unix()
	require.NoError(t, mgr.Save(&session.Session{
		AccessToken:  "valid-token",
		TokenType:    "bearer",
		Expires:      future,
		RefreshToken: "refresh-token",
	}))

	ts := auth.NewSessionTokenSource(mgr, cfg)
	tok, err := ts.Token()
	require.NoError(t, err)
	assert.Equal(t, "valid-token", tok.AccessToken)
}

func TestSessionTokenSource_RefreshExpired(t *testing.T) {
	var refreshCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "refresh_token", r.FormValue("grant_type"))
		require.Equal(t, "old-refresh", r.FormValue("refresh_token"))
		refreshCalled = true
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-token",
			"token_type":    "bearer",
			"expires_in":    3600,
			"refresh_token": "new-refresh",
		})
	}))
	defer server.Close()

	cfg := loadTestConfig(t)
	cfg.API.AuthURL = ""
	cfg.API.OAuth2TokenURL = server.URL + "/token"
	store := session.NewMemStore()
	mgr := session.NewWithStore(cfg, store)

	past := time.Now().Add(-time.Hour).Unix()
	require.NoError(t, mgr.Save(&session.Session{
		AccessToken:  "old-token",
		TokenType:    "bearer",
		Expires:      past,
		RefreshToken: "old-refresh",
	}))

	ts := auth.NewSessionTokenSource(mgr, cfg)
	tok, err := ts.Token()
	require.NoError(t, err)
	assert.True(t, refreshCalled)
	assert.Equal(t, "new-token", tok.AccessToken)
}

// TestSessionTokenSource_NoSession: Token() returns error when no session has been saved.
func TestSessionTokenSource_NoSession(t *testing.T) {
	cfg := loadTestConfig(t)
	mgr := session.NewWithStore(cfg, session.NewMemStore())

	ts := auth.NewSessionTokenSource(mgr, cfg)
	_, err := ts.Token()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not logged in")
}

// TestSessionTokenSource_EmptyAccessToken: Token() returns error when session has no access token.
func TestSessionTokenSource_EmptyAccessToken(t *testing.T) {
	cfg := loadTestConfig(t)
	mgr := session.NewWithStore(cfg, session.NewMemStore())
	require.NoError(t, mgr.Save(&session.Session{AccessToken: "", RefreshToken: "r"}))

	ts := auth.NewSessionTokenSource(mgr, cfg)
	_, err := ts.Token()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not logged in")
}

// TestSessionTokenSource_RefreshServerError: when the token server returns non-200, refresh fails with an error.
func TestSessionTokenSource_RefreshServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := loadTestConfig(t)
	cfg.API.AuthURL = ""
	cfg.API.OAuth2TokenURL = server.URL + "/token"
	mgr := session.NewWithStore(cfg, session.NewMemStore())

	past := time.Now().Add(-time.Hour).Unix()
	require.NoError(t, mgr.Save(&session.Session{
		AccessToken:  "old",
		Expires:      past,
		RefreshToken: "old-refresh",
	}))

	ts := auth.NewSessionTokenSource(mgr, cfg)
	_, err := ts.Token()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// TestSessionTokenSource_RefreshKeepsExistingRefreshToken: when the server omits refresh_token,
// the original refresh token is preserved.
func TestSessionTokenSource_RefreshKeepsExistingRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "new-access",
			"token_type":   "bearer",
			"expires_in":   3600,
			// refresh_token intentionally omitted
		})
	}))
	defer server.Close()

	cfg := loadTestConfig(t)
	cfg.API.AuthURL = ""
	cfg.API.OAuth2TokenURL = server.URL + "/token"
	mgr := session.NewWithStore(cfg, session.NewMemStore())

	past := time.Now().Add(-time.Hour).Unix()
	require.NoError(t, mgr.Save(&session.Session{
		AccessToken:  "old",
		Expires:      past,
		RefreshToken: "keep-me",
	}))

	ts := auth.NewSessionTokenSource(mgr, cfg)
	tok, err := ts.Token()
	require.NoError(t, err)
	assert.Equal(t, "new-access", tok.AccessToken)
	assert.Equal(t, "keep-me", tok.RefreshToken)
}

func loadTestConfig(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile("../../integration-tests/config.yaml")
	require.NoError(t, err)
	cfg, err := config.FromYAML(data)
	require.NoError(t, err)
	return cfg
}
