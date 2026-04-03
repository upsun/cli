package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cobrahelp "github.com/upsun/cli/commands/cobrahelp"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

func NewAPITokenLoginCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth:api-token-login",
		Short: "Log in using an API token",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var apiToken string
			if len(args) > 0 {
				apiToken = args[0]
			} else {
				fmt.Fprint(cmd.ErrOrStderr(), "Enter your API token: ")
				if _, err := fmt.Fscan(cmd.InOrStdin(), &apiToken); err != nil {
					return fmt.Errorf("read API token: %w", err)
				}
			}
			apiToken = strings.TrimSpace(apiToken)

			s, err := apiTokenToSession(cmd.Context(), cfg, apiToken)
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
			}

			mgr, err := session.New(cfg)
			if err != nil {
				return err
			}
			if err := mgr.SetAPIToken(apiToken); err != nil {
				return err
			}
			if err := mgr.Save(s); err != nil {
				return err
			}

			fmt.Fprintln(cmd.ErrOrStderr(), "You are logged in.")
			return printUserInfo(cmd.Context(), mgr, cfg, cmd.ErrOrStderr())
		},
	}
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}

// apiTokenToSession exchanges an API token for OAuth2 tokens and returns a session.
func apiTokenToSession(ctx context.Context, cfg *config.Config, apiToken string) (*session.Session, error) {
	tokenURL := oauth2TokenURL(cfg)
	if tokenURL == "" {
		return nil, fmt.Errorf("no OAuth2 token URL configured")
	}

	data := url.Values{
		"grant_type": {"api_token"},
		"api_token":  {apiToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("api token exchange: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cfg.API.OAuth2ClientID != "" {
		req.SetBasicAuth(cfg.API.OAuth2ClientID, "")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api token exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api token exchange: server returned %d", resp.StatusCode)
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("api token exchange: decode response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("api token exchange: %s", result.Error)
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Unix()
	return &session.Session{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		Expires:      expiry,
		RefreshToken: result.RefreshToken,
	}, nil
}
