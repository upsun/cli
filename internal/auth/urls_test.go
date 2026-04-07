package auth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
)

// minCfg returns a minimal config with the given env prefix and auth URLs set.
func minCfg(envPrefix, authURL, tokenURL, authorizeURL, revokeURL string) *config.Config {
	cfg := &config.Config{}
	cfg.Application.EnvPrefix = envPrefix
	cfg.API.AuthURL = authURL
	cfg.API.OAuth2TokenURL = tokenURL
	cfg.API.OAuth2AuthorizeURL = authorizeURL
	cfg.API.OAuth2RevokeURL = revokeURL
	return cfg
}

// TestOAuth2TokenURL_EnvOverride: AUTH_URL env var takes priority over config.
func TestOAuth2TokenURL_EnvOverride(t *testing.T) {
	t.Setenv("UPSUN_CLI_AUTH_URL", "https://env-auth.example.com")
	cfg := minCfg("UPSUN_CLI_", "https://config-auth.example.com", "https://fallback.example.com/token", "", "")

	assert.Equal(t, "https://env-auth.example.com/oauth2/token", auth.OAuth2TokenURL(cfg))
}

// TestOAuth2TokenURL_ConfigAuthURL: cfg.API.AuthURL used when no env var.
func TestOAuth2TokenURL_ConfigAuthURL(t *testing.T) {
	cfg := minCfg("UPSUN_CLI_", "https://config-auth.example.com", "https://fallback.example.com/token", "", "")

	assert.Equal(t, "https://config-auth.example.com/oauth2/token", auth.OAuth2TokenURL(cfg))
}

// TestOAuth2TokenURL_FallbackField: falls back to cfg.API.OAuth2TokenURL when no base URL.
func TestOAuth2TokenURL_FallbackField(t *testing.T) {
	cfg := minCfg("UPSUN_CLI_", "", "https://fallback.example.com/token", "", "")

	assert.Equal(t, "https://fallback.example.com/token", auth.OAuth2TokenURL(cfg))
}

// TestOAuth2TokenURL_TrailingSlashStripped: trailing slash on AUTH_URL is stripped before appending path.
func TestOAuth2TokenURL_TrailingSlashStripped(t *testing.T) {
	t.Setenv("UPSUN_CLI_AUTH_URL", "https://auth.example.com/")
	cfg := minCfg("UPSUN_CLI_", "", "", "", "")

	assert.Equal(t, "https://auth.example.com/oauth2/token", auth.OAuth2TokenURL(cfg))
}

// TestOAuth2AuthorizeURL_EnvOverride mirrors the token URL env precedence.
func TestOAuth2AuthorizeURL_EnvOverride(t *testing.T) {
	t.Setenv("UPSUN_CLI_AUTH_URL", "https://env-auth.example.com")
	cfg := minCfg("UPSUN_CLI_", "", "", "https://fallback.example.com/authorize", "")

	assert.Equal(t, "https://env-auth.example.com/oauth2/authorize", auth.OAuth2AuthorizeURL(cfg))
}

// TestOAuth2AuthorizeURL_Fallback: falls back to cfg.API.OAuth2AuthorizeURL.
func TestOAuth2AuthorizeURL_Fallback(t *testing.T) {
	cfg := minCfg("UPSUN_CLI_", "", "", "https://fallback.example.com/authorize", "")

	assert.Equal(t, "https://fallback.example.com/authorize", auth.OAuth2AuthorizeURL(cfg))
}

// TestOAuth2RevokeURL_EnvOverride mirrors the token URL env precedence.
func TestOAuth2RevokeURL_EnvOverride(t *testing.T) {
	t.Setenv("UPSUN_CLI_AUTH_URL", "https://env-auth.example.com")
	cfg := minCfg("UPSUN_CLI_", "", "", "", "https://fallback.example.com/revoke")

	assert.Equal(t, "https://env-auth.example.com/oauth2/revoke", auth.OAuth2RevokeURL(cfg))
}

// TestOAuth2RevokeURL_Fallback: falls back to cfg.API.OAuth2RevokeURL.
func TestOAuth2RevokeURL_Fallback(t *testing.T) {
	cfg := minCfg("UPSUN_CLI_", "", "", "", "https://fallback.example.com/revoke")

	assert.Equal(t, "https://fallback.example.com/revoke", auth.OAuth2RevokeURL(cfg))
}

// TestOAuth2TokenURL_EnvPrefixIsolation: a different prefix's env var must NOT affect resolution.
func TestOAuth2TokenURL_EnvPrefixIsolation(t *testing.T) {
	t.Setenv("OTHER_CLI_AUTH_URL", "https://wrong.example.com")
	cfg := minCfg("UPSUN_CLI_", "", "https://correct.example.com/token", "", "")

	assert.Equal(t, "https://correct.example.com/token", auth.OAuth2TokenURL(cfg))
}
