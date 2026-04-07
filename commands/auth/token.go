package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cobrahelp "github.com/upsun/cli/commands/cobrahelp"
	internalauth "github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

func NewTokenCommand(cfg *config.Config) *cobra.Command {
	var (
		header bool
		noWarn bool
	)
	cmd := &cobra.Command{
		Use:    "auth:token",
		Short:  "Obtain an OAuth 2 access token for API requests",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !noWarn {
				fmt.Fprintln(cmd.ErrOrStderr(), "Warning: keep access tokens secret.")
			}

			// Check for an API token in the environment or session.
			apiToken := os.Getenv(cfg.Application.EnvPrefix + "TOKEN")
			if apiToken == "" {
				mgr, err := session.New(cfg)
				if err != nil {
					return err
				}
				apiToken, err = mgr.GetAPIToken()
				if err != nil {
					return err
				}
			}

			var accessToken string
			if apiToken != "" {
				// Exchange the API token for an OAuth2 access token.
				s, err := exchangeAPIToken(cmd.Context(), cfg, apiToken)
				if err != nil {
					return err
				}
				accessToken = s.AccessToken
			} else {
				// Fall back to the session token source (browser login).
				mgr, err := session.New(cfg)
				if err != nil {
					return err
				}
				ts := internalauth.NewSessionTokenSource(mgr, cfg)
				tok, err := ts.TokenContext(cmd.Context())
				if err != nil {
					return err
				}
				accessToken = tok.AccessToken
			}

			out := accessToken
			if header {
				out = "Authorization: Bearer " + out
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&header, "header", "H", false, `Output the token as an HTTP "Authorization: Bearer" header`)
	cmd.Flags().BoolVarP(&noWarn, "no-warn", "W", false, "Suppress the warning message")
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}

// exchangeAPIToken exchanges an API token for OAuth2 tokens and returns the resulting session.
func exchangeAPIToken(ctx context.Context, cfg *config.Config, apiToken string) (*session.Session, error) {
	tokenURL := internalauth.OAuth2TokenURL(cfg)
	if tokenURL == "" {
		return nil, fmt.Errorf("no OAuth2 token URL configured")
	}

	data := url.Values{
		"grant_type": {"api_token"},
		"api_token":  {apiToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("exchange API token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cfg.API.OAuth2ClientID != "" {
		req.SetBasicAuth(cfg.API.OAuth2ClientID, "")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange API token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchange API token: server returned %d", resp.StatusCode)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("exchange API token: decode response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("exchange API token: %s", result.Error)
	}
	if result.AccessToken == "" {
		return nil, fmt.Errorf("exchange API token: no access token in response")
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Unix()
	return &session.Session{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		Expires:      expiry,
		RefreshToken: result.RefreshToken,
	}, nil
}
