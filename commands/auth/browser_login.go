// commands/auth/browser_login.go
package auth

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cobrahelp "github.com/upsun/cli/commands/cobrahelp"
	internalauth "github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

func NewBrowserLoginCommand(cfg *config.Config) *cobra.Command {
	var (
		force   bool
		methods []string
		maxAge  int
	)
	cmd := &cobra.Command{
		Use:     "auth:browser-login",
		Aliases: []string{"login"},
		Short:   "Log in via a browser",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// If an API token is configured, browser login is not applicable.
			if apiToken := os.Getenv(cfg.Application.EnvPrefix + "TOKEN"); apiToken != "" {
				return fmt.Errorf("Cannot log in via the browser while an API token is set (%sTOKEN)", cfg.Application.EnvPrefix)
			}

			mgr, err := session.New(cfg)
			if err != nil {
				return err
			}

			// Also check for an API token in the session.
			if storedToken, err := mgr.GetAPIToken(); err == nil && storedToken != "" {
				return fmt.Errorf("Cannot log in via the browser while an API token is configured")
			}

			// Non-interactive guard.
			if os.Getenv(cfg.Application.EnvPrefix+"NO_INTERACTION") != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Non-interactive use of this command is not supported.")
				return fmt.Errorf("non-interactive use of this command is not supported")
			}

			printSessionID(cmd.ErrOrStderr(), cfg, mgr)

			hasMaxAge := cmd.Flags().Changed("max-age")

			// Check if already logged in (unless --force).
			if !force && len(methods) == 0 && !hasMaxAge {
				s, err := mgr.Load()
				if err == nil && s != nil && s.AccessToken != "" && time.Now().Unix() < s.Expires {
					fmt.Fprintf(cmd.ErrOrStderr(), "You are already logged in as a user.\n")
					fmt.Fprint(cmd.ErrOrStderr(), "Log in anyway? [y/N] ")
					scanner := bufio.NewScanner(cmd.InOrStdin())
					scanner.Scan()
					answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
					if answer != "y" && answer != "yes" {
						return nil
					}
					force = true
				}
			}

			flow := internalauth.NewBrowserFlow(cfg)
			opts := internalauth.BrowserFlowOptions{
				Force:   force,
				Methods: methods,
				Stderr:  cmd.ErrOrStderr(),
				OnCodeReceived: func() {
					fmt.Fprintln(cmd.ErrOrStderr(), "Login information received. Verifying...")
				},
			}
			if hasMaxAge {
				opts.MaxAge = &maxAge
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "\nHelp:\n  Leave this command running during login.\n  If you need to quit, use Ctrl+C.\n\n")

			s, err := flow.Run(cmd.Context(), opts)
			if err != nil {
				return err
			}

			if err := mgr.Save(s); err != nil {
				return err
			}

			if s.RefreshToken == "" {
				clientID := cfg.API.OAuth2ClientID
				fmt.Fprintln(cmd.ErrOrStderr(), "")
				fmt.Fprintln(cmd.ErrOrStderr(), "Warning:")
				fmt.Fprintln(cmd.ErrOrStderr(), "No refresh token is available. This will cause frequent login errors.")
				fmt.Fprintln(cmd.ErrOrStderr(), "Please contact support.")
				fmt.Fprintf(cmd.ErrOrStderr(), "For internal use: the OAuth 2 client is probably misconfigured (client ID: %s).\n", clientID)
			}

			fmt.Fprintln(cmd.ErrOrStderr(), "You are logged in.")

			if err := printUserInfo(cmd.Context(), mgr, cfg, cmd.ErrOrStderr()); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not retrieve user info: %v\n", err)
			}

			delegateSSHFinalization(cmd.Context(), cfg, cmd)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Log in again, even if already logged in")
	cmd.Flags().StringArrayVar(&methods, "method", nil, "Require specific authentication method(s)")
	cmd.Flags().IntVar(&maxAge, "max-age", 0, "Maximum age (seconds) of the web authentication session")
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}
