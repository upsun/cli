package auth

import (
	"bufio"
	"fmt"
	"strings"
	"unicode"

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
			country, _ := info["country"].(string)

			scanner := bufio.NewScanner(cmd.InOrStdin())

			// Choose method.
			fmt.Fprintln(cmd.ErrOrStderr(), "Choose a verification method:")
			fmt.Fprintln(cmd.ErrOrStderr(), "  [0] SMS (default)")
			fmt.Fprintln(cmd.ErrOrStderr(), "  [1] WhatsApp message")
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

			// Get phone number — re-prompt up to 5 times on invalid input (PHP parity: askInput with setMaxAttempts(5)).
			const maxAttempts = 5
			var e164 string
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				fmt.Fprint(cmd.ErrOrStderr(), "Enter your phone number (international format, e.g. +1 415 555 0100): ")
				scanner.Scan()
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("read input: %w", err)
				}
				rawNumber := strings.TrimSpace(scanner.Text())
				num, parseErr := phonenumbers.Parse(rawNumber, country)
				if rawNumber == "" || parseErr != nil || !phonenumbers.IsValidNumber(num) {
					fmt.Fprintln(cmd.ErrOrStderr(), "The phone number is not valid.")
					if attempt == maxAttempts {
						return fmt.Errorf("too many invalid phone numbers")
					}
					continue
				}
				e164 = phonenumbers.Format(num, phonenumbers.E164)
				break
			}

			sid, err := apiClient.SendPhoneVerification(ctx, userID, e164, method)
			if err != nil {
				return fmt.Errorf("send verification: %w", err)
			}

			switch method {
			case "call":
				fmt.Fprintf(cmd.ErrOrStderr(), "Calling the number %s with a verification code.\n", e164)
			case "sms":
				fmt.Fprintf(cmd.ErrOrStderr(), "A verification code has been sent using SMS to the number: %s\n", e164)
			case "whatsapp":
				fmt.Fprintf(cmd.ErrOrStderr(), "A verification code has been sent using WhatsApp to the number: %s\n", e164)
			}
			fmt.Fprintln(cmd.ErrOrStderr())

			// Get verification code — re-prompt up to 5 times on invalid input or rejected code (PHP parity).
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				fmt.Fprint(cmd.ErrOrStderr(), "Enter the verification code: ")
				scanner.Scan()
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("read input: %w", err)
				}
				code := strings.TrimSpace(scanner.Text())
				isNumeric := code != ""
				for _, c := range code {
					if !unicode.IsDigit(c) {
						isNumeric = false
						break
					}
				}
				if !isNumeric {
					fmt.Fprintln(cmd.ErrOrStderr(), "Invalid verification code")
					if attempt == maxAttempts {
						return fmt.Errorf("too many invalid verification codes")
					}
					continue
				}
				if err := apiClient.VerifyPhone(ctx, userID, sid, code); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "Invalid verification code")
					if attempt == maxAttempts {
						return fmt.Errorf("too many invalid verification codes")
					}
					continue
				}
				break
			}

			// Verify the status was actually updated.
			if err := apiClient.CheckVerificationStatus(ctx); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "Phone verification succeeded but the status check failed.")
				return fmt.Errorf("verification status check failed: %w", err)
			}

			fmt.Fprintln(cmd.ErrOrStderr(), "Your phone number has been successfully verified.")
			return nil
		},
	}
	cobrahelp.SetPhpStyle(cmd)
	return cmd
}
