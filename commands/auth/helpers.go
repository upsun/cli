package auth

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/oauth2"

	internalauth "github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/api"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

// resolveBaseURL returns the API base URL, preferring the env var override.
func resolveBaseURL(cfg *config.Config) string {
	if v := os.Getenv(cfg.Application.EnvPrefix + "API_URL"); v != "" {
		return v
	}
	return cfg.API.BaseURL
}

// newAPIClient creates an authenticated API client for commands.
//
// Auth priority:
//  1. API token from env var ({EnvPrefix}TOKEN) or session storage — exchanged for OAuth access token.
//  2. Session OAuth token — used directly.
func newAPIClient(ctx context.Context, mgr *session.Manager, cfg *config.Config) (*api.Client, error) {
	// Check for API token in env or session storage.
	apiToken := os.Getenv(cfg.Application.EnvPrefix + "TOKEN")
	if apiToken == "" {
		var err error
		apiToken, err = mgr.GetAPIToken()
		if err != nil {
			return nil, err
		}
	}

	var httpClient *oauth2.Transport
	if apiToken != "" {
		// Exchange the API token for an OAuth2 access token.
		s, err := exchangeAPIToken(ctx, cfg, apiToken)
		if err != nil {
			return nil, err
		}
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: s.AccessToken})
		httpClient = &oauth2.Transport{Source: ts}
	} else {
		// Fall back to session-based OAuth token source.
		authClient, err := internalauth.NewClient(ctx, mgr, cfg)
		if err != nil {
			return nil, err
		}
		return api.NewClient(resolveBaseURL(cfg), authClient.HTTPClient)
	}

	return api.NewClient(resolveBaseURL(cfg), oauth2.NewClient(ctx, httpClient.Source))
}

// printUserInfo fetches and prints the current user's info to w (used post-login).
func printUserInfo(ctx context.Context, mgr *session.Manager, cfg *config.Config, w io.Writer) error {
	apiClient, err := newAPIClient(ctx, mgr, cfg)
	if err != nil {
		return err
	}
	info, err := apiClient.GetMyUser(ctx, false)
	if err != nil {
		return err
	}
	username, _ := info["username"].(string)
	email, _ := info["email"].(string)
	fmt.Fprintf(w, "Logged in as: %s (%s)\n", username, email)
	return nil
}

// printTable writes a two-column property/value table to w.
func printTable(w io.Writer, properties []string, data map[string]interface{}) {
	col1 := len("Property")
	col2 := len("Value")
	for _, p := range properties {
		if len(p) > col1 {
			col1 = len(p)
		}
		v := formatValue(data[p])
		if len(v) > col2 {
			col2 = len(v)
		}
	}
	sep := "+" + strings.Repeat("-", col1+2) + "+" + strings.Repeat("-", col2+2) + "+"
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "| %-*s | %-*s |\n", col1, "Property", col2, "Value")
	fmt.Fprintln(w, sep)
	for _, p := range properties {
		v := formatValue(data[p])
		fmt.Fprintf(w, "| %-*s | %-*s |\n", col1, p, col2, v)
	}
	fmt.Fprintln(w, sep)
}

// formatValue converts an interface{} to a display string.
func formatValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", val))
	}
}
