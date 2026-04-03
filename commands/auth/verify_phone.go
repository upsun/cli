package auth

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/nyaruka/phonenumbers"
	"github.com/spf13/cobra"

	cobrahelp "github.com/upsun/cli/commands/cobrahelp"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

func NewVerifyPhoneNumberCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "auth:verify-phone-number",
		Short:  "Verify your phone number interactively",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			mgr, err := session.New(cfg)
			if err != nil {
				return err
			}
			apiClient, err := newAPIClient(ctx, mgr, cfg)
			if err != nil {
				return err
			}

			info, err := apiClient.GetMyUser(ctx, true)
			if err != nil {
				return err
			}
			if verified, _ := info["phone_number_verified"].(bool); verified {
				fmt.Fprintln(cmd.ErrOrStderr(), "Your user account already has a verified phone number.")
				return nil
			}
			userID, _ := info["id"].(string)
			if userID == "" {
				return fmt.Errorf("could not determine user ID")
			}

			scanner := bufio.NewScanner(cmd.InOrStdin())

			// Choose method.
			fmt.Fprintln(cmd.ErrOrStderr(), "Choose a verification method:")
			fmt.Fprintln(cmd.ErrOrStderr(), "  [0] SMS")
			fmt.Fprintln(cmd.ErrOrStderr(), "  [1] WhatsApp")
			fmt.Fprintln(cmd.ErrOrStderr(), "  [2] Call")
			fmt.Fprint(cmd.ErrOrStderr(), "Enter a number (default: 0): ")
			scanner.Scan()
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			choice := strings.TrimSpace(scanner.Text())
			var method string
			switch choice {
			case "", "0":
				method = "sms"
			case "1":
				method = "whatsapp"
			case "2":
				method = "call"
			default:
				method = choice
			}

			// Get phone number.
			fmt.Fprint(cmd.ErrOrStderr(), "Enter your phone number (international format, e.g. +1 415 555 0100): ")
			scanner.Scan()
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			rawNumber := strings.TrimSpace(scanner.Text())
			if rawNumber == "" {
				return fmt.Errorf("no phone number provided")
			}

			num, err := phonenumbers.Parse(rawNumber, "")
			if err != nil || !phonenumbers.IsValidNumber(num) {
				return fmt.Errorf("invalid phone number: %s", rawNumber)
			}
			e164 := phonenumbers.Format(num, phonenumbers.E164)

			sid, err := apiClient.SendPhoneVerification(ctx, userID, e164, method)
			if err != nil {
				return fmt.Errorf("send verification: %w", err)
			}

			fmt.Fprint(cmd.ErrOrStderr(), "Enter the verification code: ")
			scanner.Scan()
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			code := strings.TrimSpace(scanner.Text())
			if code == "" {
				return fmt.Errorf("no verification code provided")
			}

			if err := apiClient.VerifyPhone(ctx, userID, sid, code); err != nil {
				return fmt.Errorf("verify phone: %w", err)
			}

			fmt.Fprintln(cmd.ErrOrStderr(), "Phone number verified successfully.")
			return nil
		},
	}
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}
