package auth

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cobrahelp "github.com/upsun/cli/commands/cobrahelp"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

func NewInfoCommand(cfg *config.Config) *cobra.Command {
	var (
		noAutoLogin bool
		property    string
		refresh     bool
	)
	cmd := &cobra.Command{
		Use:   "auth:info [property]",
		Short: "Display your account information",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				if property != "" {
					return fmt.Errorf("cannot use both the <property> argument and --property option")
				}
				property = args[0]
			}

			ctx := cmd.Context()
			mgr, err := session.New(cfg)
			if err != nil {
				return err
			}

			if noAutoLogin {
				// Consider logged in if TOKEN env var, stored API token, or OAuth session exists.
				if os.Getenv(cfg.Application.EnvPrefix+"TOKEN") != "" {
					// Logged in via env var, proceed.
				} else {
					apiToken, _ := mgr.GetAPIToken()
					if apiToken == "" {
						s, _ := mgr.Load()
						if s == nil || s.AccessToken == "" {
							return nil
						}
					}
				}
			}

			// Check login state before making API calls.
			if os.Getenv(cfg.Application.EnvPrefix+"TOKEN") == "" {
				apiToken, _ := mgr.GetAPIToken()
				if apiToken == "" {
					if s, err := mgr.Load(); err == nil && (s == nil || s.AccessToken == "") {
						return fmt.Errorf("not logged in. Run '%s login' to authenticate", cfg.Application.Executable)
					}
				}
			}

			apiClient, err := newAPIClient(ctx, mgr, cfg)
			if err != nil {
				return err
			}

			info, err := apiClient.GetMyUser(ctx, refresh)
			if err != nil {
				return err
			}

			// Handle deprecated property aliases.
			if property == "display_name" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Deprecated: the \"display_name\" property has been replaced by \"first_name\" and \"last_name\".")
				firstName, _ := info["first_name"].(string)
				lastName, _ := info["last_name"].(string)
				fmt.Fprintln(cmd.OutOrStdout(), firstName+" "+lastName)
				return nil
			}
			if property == "mail" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Deprecated: the \"mail\" property is now named \"email\".")
				property = "email"
			}
			if property == "uuid" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Deprecated: the \"uuid\" property is now named \"id\".")
				property = "id"
			}

			if property != "" {
				val, ok := info[property]
				if !ok {
					return fmt.Errorf("property not found: %s", property)
				}
				fmt.Fprintln(cmd.OutOrStdout(), formatValue(val))
				return nil
			}

			// Table output.
			properties := []string{"id", "first_name", "last_name", "username", "email", "phone_number_verified"}
			printTable(cmd.OutOrStdout(), properties, info)

			// Show session info when applicable.
			sessionID := mgr.SessionID()
			ids, _ := mgr.List()
			if sessionID != "default" || len(ids) > 1 {
				fmt.Fprintln(cmd.ErrOrStderr())
				fmt.Fprintf(cmd.ErrOrStderr(), "The current session ID is: %s\n", sessionID)
				if os.Getenv(cfg.Application.EnvPrefix+"SESSION_ID") == "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "Change this using: %s session:switch\n", cfg.Application.Executable)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noAutoLogin, "no-auto-login", false, "Skip auto login; exit 0 if not logged in")
	cmd.Flags().StringVarP(&property, "property", "P", "", "The account property to view")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Refresh the cache")
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}
