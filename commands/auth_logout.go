package commands

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
)

func newAPITokenLoginCommand(cnf *config.Config) *cobra.Command {
	help := fmt.Sprintf("Use this command to log in to your %s account using an API token.", cnf.Service.Name)
	help += fmt.Sprintf("\n\nAlternatively, to log in to the CLI with a browser, run:\n    %s",
		color.GreenString(cnf.Application.Executable+" auth:browser-login"))
	return &cobra.Command{
		Use:   "auth:api-token-login",
		Short: "Log in using an API token",
		Long:  help,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stderr := cmd.ErrOrStderr()
			m, err := newAuthManager(cnf, stderr)
			if err != nil {
				return err
			}
			if has, err := m.HasConfiguredToken(); err != nil {
				return err
			} else if has {
				fmt.Fprintln(stderr, "An API token is already set via config")
				return &exitError{code: 1}
			}
			if !isInteractive(cmd) {
				fmt.Fprintln(stderr, "Non-interactive use of this command is not supported.")
				fmt.Fprintln(stderr, "\n"+nonInteractiveAuthHelp(cnf))
				return &exitError{code: 1}
			}

			const maxAttempts = 5
			for range maxAttempts {
				fmt.Fprint(stderr, "Please enter an API token:\n> ")
				apiToken, err := readSecret(cmd)
				if err != nil {
					return err
				}
				apiToken = strings.TrimSpace(apiToken)
				if apiToken == "" {
					fmt.Fprintln(stderr, color.RedString("The token cannot be empty"))
					continue
				}
				entry, err := m.OAuth.ExchangeAPIToken(cmd.Context(), apiToken)
				if err != nil {
					var oerr *auth.OAuthError
					if !errors.As(err, &oerr) {
						return err
					}
					fmt.Fprintln(stderr, color.RedString(err.Error()))
					continue
				}
				fmt.Fprintln(stderr)
				fmt.Fprintln(stderr, "The API token is valid.")
				return saveLogin(cmd, cnf, m, entry, apiToken)
			}
			// Each error has been printed.
			return &exitError{code: 1}
		},
	}
}

func newLogoutCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "auth:logout",
		Aliases: []string{"logout"},
		Short:   "Log out",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			all, _ := cmd.Flags().GetBool("all")
			other, _ := cmd.Flags().GetBool("other")
			return runLogout(cmd, cnf, all, other)
		},
	}
	cmd.Flags().BoolP("all", "a", false, "Log out from all local sessions")
	cmd.Flags().Bool("other", false, "Log out from other local sessions")
	return cmd
}

func runLogout(cmd *cobra.Command, cnf *config.Config, all, other bool) error {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()
	m, err := newAuthManager(cnf, stderr)
	if err != nil {
		return err
	}

	// API tokens set via config cannot be removed using this command.
	if has, err := m.HasConfiguredToken(); err != nil {
		return err
	} else if has {
		fmt.Fprintln(stderr, color.YellowString("Warning: an API token is set via config"))
	}

	current := m.Settings.SessionID
	ids, err := m.SessionIDs(ctx)
	if err != nil {
		return err
	}

	if other && !all {
		fmt.Fprintf(stderr, "The current session ID is: %s\n", color.GreenString(current))
		others := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == current })
		if len(others) == 0 {
			fmt.Fprintln(stderr, "No other sessions exist.")
			return nil
		}
		fmt.Fprintln(stderr)
		for _, id := range others {
			if err := m.Logout(ctx, id); err != nil {
				return err
			}
		}
		if err := clearLegacySessionFiles(cnf, current, others, false); err != nil {
			return err
		}
		for _, id := range others {
			fmt.Fprintf(stderr, "Logged out from session: %s\n", color.GreenString(id))
		}
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "All other sessions have been deleted.")
		return nil
	}

	if err := m.Logout(ctx, current); err != nil {
		return err
	}
	if all {
		for _, id := range ids {
			if id == current {
				continue
			}
			if err := m.Logout(ctx, id); err != nil {
				return err
			}
		}
		if err := m.DeleteAll(ctx); err != nil {
			return err
		}
		if err := clearLegacySessionFiles(cnf, current, ids, true); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "You are now logged out.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "All sessions have been deleted.")
		printSessionAdvice(cmd, cnf, m)
		return nil
	}
	if err := clearLegacySessionFiles(cnf, current, []string{current}, false); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "You are now logged out.")
	printSessionAdvice(cmd, cnf, m)

	remaining, err := m.SessionIDs(ctx)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		fmt.Fprintln(stderr)
		fmt.Fprintf(stderr, "Other sessions exist. Log out of all sessions with: %s\n",
			color.YellowString(cnf.Application.Executable+" logout --all"))
	}
	return nil
}

func printSessionAdvice(cmd *cobra.Command, cnf *config.Config, m *auth.Manager) {
	if advice := sessionAdvice(cmd.Context(), cnf, m, "Change this using"); advice != nil {
		fmt.Fprintln(cmd.ErrOrStderr())
		fmt.Fprintln(cmd.ErrOrStderr(), strings.Join(advice, "\n"))
	}
}
