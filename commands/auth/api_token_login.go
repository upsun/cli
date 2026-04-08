package auth

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

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
			// Block if TOKEN env var is already set via config.
			if os.Getenv(cfg.Application.EnvPrefix+"TOKEN") != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "An API token is already set via config")
				return fmt.Errorf("an API token is already set via config")
			}
			// Non-interactive guard only when no arg (stdin would be needed).
			if len(args) == 0 && os.Getenv(cfg.Application.EnvPrefix+"NO_INTERACTION") != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Non-interactive use of this command is not supported.")
				return fmt.Errorf("non-interactive use of this command is not supported")
			}

			var (
				apiToken string
				s        *session.Session
			)
			if len(args) > 0 {
				apiToken = strings.TrimSpace(args[0])
				var err error
				s, err = exchangeAPIToken(cmd.Context(), cfg, apiToken)
				if err != nil {
					return fmt.Errorf("login failed: %w", err)
				}
			} else {
				const maxAttempts = 5
				scanner := bufio.NewScanner(cmd.InOrStdin())
				for attempt := 1; attempt <= maxAttempts; attempt++ {
					fmt.Fprint(cmd.ErrOrStderr(), "Enter your API token: ")
					if !scanner.Scan() {
						return fmt.Errorf("read API token: %w", scanner.Err())
					}
					apiToken = strings.TrimSpace(scanner.Text())
					if apiToken == "" {
						fmt.Fprintln(cmd.ErrOrStderr(), "The token cannot be empty")
						continue
					}
					var err error
					s, err = exchangeAPIToken(cmd.Context(), cfg, apiToken)
					if err == nil {
						break
					}
					if errors.Is(err, ErrInvalidAPIToken) {
						fmt.Fprintln(cmd.ErrOrStderr(), ErrInvalidAPIToken.Error())
						if attempt == maxAttempts {
							return fmt.Errorf("login failed after %d attempts", maxAttempts)
						}
						continue
					}
					return fmt.Errorf("login failed: %w", err)
				}
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "The API token is valid.")

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
			if err := printUserInfo(cmd.Context(), mgr, cfg, cmd.ErrOrStderr()); err != nil {
				return err
			}
			delegateSSHFinalization(cmd.Context(), cfg, cmd)
			return nil
		},
	}
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}
