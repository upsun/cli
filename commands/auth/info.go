package auth

import (
	"fmt"

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
				s, err := mgr.Load()
				if err != nil || s == nil || s.AccessToken == "" {
					return nil
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
			return nil
		},
	}
	cmd.Flags().BoolVar(&noAutoLogin, "no-auto-login", false, "Skip auto login; exit 0 if not logged in")
	cmd.Flags().StringVarP(&property, "property", "P", "", "The account property to view")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Refresh the cache")
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}
