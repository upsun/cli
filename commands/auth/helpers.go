package auth

import (
	"context"
	"fmt"
	"io"
	"strings"

	internalauth "github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/api"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

// newAPIClient creates an authenticated API client for commands.
func newAPIClient(ctx context.Context, mgr *session.Manager, cfg *config.Config) (*api.Client, error) {
	authClient, err := internalauth.NewClient(ctx, mgr, cfg)
	if err != nil {
		return nil, err
	}
	return api.NewClient(cfg.API.BaseURL, authClient.HTTPClient)
}

// printUserInfo fetches and prints the current user's info to stderr (used post-login).
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
	// Calculate column widths.
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
