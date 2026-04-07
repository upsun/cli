package auth

import (
	"os"
	"strings"

	"github.com/upsun/cli/internal/config"
)

// OAuth2TokenURL resolves the OAuth2 token endpoint.
// Priority: {EnvPrefix}AUTH_URL env → cfg.API.AuthURL → cfg.API.OAuth2TokenURL
func OAuth2TokenURL(cfg *config.Config) string {
	authURL := os.Getenv(cfg.Application.EnvPrefix + "AUTH_URL")
	if authURL == "" {
		authURL = cfg.API.AuthURL
	}
	if authURL != "" {
		return strings.TrimRight(authURL, "/") + "/oauth2/token"
	}
	return cfg.API.OAuth2TokenURL
}

// OAuth2AuthorizeURL resolves the OAuth2 authorize endpoint.
// Priority: {EnvPrefix}AUTH_URL env → cfg.API.AuthURL → cfg.API.OAuth2AuthorizeURL
func OAuth2AuthorizeURL(cfg *config.Config) string {
	authURL := os.Getenv(cfg.Application.EnvPrefix + "AUTH_URL")
	if authURL == "" {
		authURL = cfg.API.AuthURL
	}
	if authURL != "" {
		return strings.TrimRight(authURL, "/") + "/oauth2/authorize"
	}
	return cfg.API.OAuth2AuthorizeURL
}

// OAuth2RevokeURL resolves the OAuth2 revocation endpoint.
// Priority: {EnvPrefix}AUTH_URL env → cfg.API.AuthURL → cfg.API.OAuth2RevokeURL
func OAuth2RevokeURL(cfg *config.Config) string {
	authURL := os.Getenv(cfg.Application.EnvPrefix + "AUTH_URL")
	if authURL == "" {
		authURL = cfg.API.AuthURL
	}
	if authURL != "" {
		return strings.TrimRight(authURL, "/") + "/oauth2/revoke"
	}
	return cfg.API.OAuth2RevokeURL
}
