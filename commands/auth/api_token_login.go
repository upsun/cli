package auth

import (
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

			s, err := exchangeAPIToken(cmd.Context(), cfg, apiToken)
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
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
			return delegateSSHFinalization(cmd.Context(), cfg, cmd)
		},
	}
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}
