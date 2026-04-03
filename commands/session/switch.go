// commands/session/switch.go
package session

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/upsun/cli/internal/config"
	internalsession "github.com/upsun/cli/internal/session"
)

var validSessionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func NewSwitchCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "session:switch [id]",
		Short:  "Switch between sessions",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Blocked if session ID comes from env.
			envKey := cfg.Application.EnvPrefix + "SESSION_ID"
			if id := os.Getenv(envKey); id != "" {
				return fmt.Errorf("the session ID is set via the environment variable %s; it cannot be changed using this command", envKey)
			}

			mgr, err := internalsession.New(cfg)
			if err != nil {
				return err
			}
			previousID := mgr.SessionID()

			var newID string
			if len(args) > 0 {
				newID = args[0]
			} else {
				// Interactive prompt.
				ids, err := mgr.List()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Current session ID: %s\n", previousID)
				if len(ids) > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "Existing sessions: %s\n", strings.Join(ids, ", "))
				}
				fmt.Fprint(cmd.ErrOrStderr(), "Enter new session ID: ")
				scanner := bufio.NewScanner(cmd.InOrStdin())
				if scanner.Scan() {
					newID = strings.TrimSpace(scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("read input: %w", err)
				}
			}

			if newID == "" {
				return fmt.Errorf("session ID cannot be empty")
			}
			if strings.HasPrefix(newID, "api-token-") {
				return fmt.Errorf("invalid session ID: %q", newID)
			}
			if !validSessionID.MatchString(newID) {
				return fmt.Errorf("invalid session ID %q: must match [a-zA-Z0-9_-]+", newID)
			}

			if newID == previousID {
				fmt.Fprintf(cmd.ErrOrStderr(), "Session ID is already set as %q\n", newID)
				return nil
			}

			if err := mgr.SetActiveSessionID(newID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Session ID changed from %q to %q\n", previousID, newID)
			return nil
		},
	}
	return cmd
}
