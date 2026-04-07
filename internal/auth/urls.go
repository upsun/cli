package auth

import (
	"os"
	"strings"

	"github.com/upsun/cli/internal/config"
)

// resolveAuthBase returns the OAuth2 server base URL.
// Priority: {EnvPrefix}AUTH_URL env → cfg.API.AuthURL
func resolveAuthBase(cfg *config.Config) string {
	if v := os.Getenv(cfg.Application.EnvPrefix + "AUTH_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(cfg.API.AuthURL, "/")
}

// OAuth2TokenURL resolves the OAuth2 token endpoint.
func OAuth2TokenURL(cfg *config.Config) string {
	if base := resolveAuthBase(cfg); base != "" {
		return base + "/oauth2/token"
	}
	return cfg.API.OAuth2TokenURL
}

// OAuth2AuthorizeURL resolves the OAuth2 authorize endpoint.
func OAuth2AuthorizeURL(cfg *config.Config) string {
	if base := resolveAuthBase(cfg); base != "" {
		return base + "/oauth2/authorize"
	}
	return cfg.API.OAuth2AuthorizeURL
}

// OAuth2RevokeURL resolves the OAuth2 revocation endpoint.
func OAuth2RevokeURL(cfg *config.Config) string {
	if base := resolveAuthBase(cfg); base != "" {
		return base + "/oauth2/revoke"
	}
	return cfg.API.OAuth2RevokeURL
}
