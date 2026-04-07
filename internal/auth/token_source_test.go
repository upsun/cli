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

func loadTestConfig(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile("../../integration-tests/config.yaml")
	require.NoError(t, err)
	cfg, err := config.FromYAML(data)
	require.NoError(t, err)
	return cfg
}
