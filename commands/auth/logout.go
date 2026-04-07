package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	cobrahelp "github.com/upsun/cli/commands/cobrahelp"
	internalauth "github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

// revokeSession POSTs the access token to the OAuth2 revocation endpoint.
// Network or server errors are printed as warnings — local cleanup always proceeds.
func revokeSession(ctx context.Context, mgr *session.Manager, cfg *config.Config, warn func(string)) {
	s, err := mgr.Load()
	if err != nil || s == nil || s.AccessToken == "" {
		return
	}
	revokeURL := internalauth.OAuth2RevokeURL(cfg)
	if revokeURL == "" {
		return
	}
	body := url.Values{
		"token":           {s.AccessToken},
		"token_type_hint": {"access_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revokeURL, strings.NewReader(body.Encode()))
	if err != nil {
		warn(fmt.Sprintf("Warning: could not build revoke request: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		warn(fmt.Sprintf("Warning: could not revoke token: %v", err))
		return
	}
	resp.Body.Close()
}

func NewLogoutCommand(cfg *config.Config) *cobra.Command {
	var (
		all   bool
		other bool
	)
	cmd := &cobra.Command{
		Use:     "auth:logout",
		Aliases: []string{"logout"},
		Short:   "Log out",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mgr, err := session.New(cfg)
			if err != nil {
				return err
			}

			if os.Getenv(cfg.Application.EnvPrefix+"TOKEN") != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Warning: an API token is set via config")
			}

			warn := func(msg string) { fmt.Fprintln(cmd.ErrOrStderr(), msg) }

			if other && !all {
				currentID := mgr.SessionID()
				fmt.Fprintf(cmd.ErrOrStderr(), "The current session ID is: %s\n", currentID)
				ids, err := mgr.List()
				if err != nil {
					return err
				}
				var others []string
				for _, id := range ids {
					if id != currentID {
						others = append(others, id)
					}
				}
				if len(others) == 0 {
					fmt.Fprintln(cmd.ErrOrStderr(), "No other sessions exist.")
					return nil
				}
				fmt.Fprintln(cmd.ErrOrStderr())
				for _, id := range others {
					sub := session.NewWithID(cfg, id)
					revokeSession(cmd.Context(), sub, cfg, warn)
					if err := sub.Delete(); err != nil {
						return fmt.Errorf("delete session %q: %w", id, err)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Logged out from session: %s\n", id)
				}
				fmt.Fprintln(cmd.ErrOrStderr())
				fmt.Fprintln(cmd.ErrOrStderr(), "All other sessions have been deleted.")
				return nil
			}

			revokeSession(cmd.Context(), mgr, cfg, warn)
			if err := mgr.Delete(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "You are now logged out.")

			if all {
				if err := mgr.DeleteAll(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "\nAll sessions have been deleted.")
				return nil
			}

			ids, err := mgr.List()
			if err != nil {
				return err
			}
			if len(ids) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "\nOther sessions exist. Use '%s logout --all' to log out from all.\n",
					cfg.Application.Executable)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Log out from all local sessions")
	cmd.Flags().BoolVar(&other, "other", false, "Log out from other local sessions")
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}
