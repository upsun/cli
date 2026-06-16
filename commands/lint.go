package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/lint"
)

// errLintFailed signals that the configuration has errors, for a non-zero exit
// code. Its message is empty because output is printed by the command itself.
var errLintFailed = errors.New("")

func newLintCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "lint [path]",
		Short:         "Validate project configuration",
		Aliases:       []string{"validate", "app:config-validate"},
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          runLint,
	}
	cmd.Flags().Bool("stdin", false, "Read merged Flex configuration from standard input")
	cmd.Flags().String("format", "text", "Output format: text or json")
	cmd.SetHelpFunc(func(_ *cobra.Command, _ []string) {
		internalCmd := innerAppConfigValidateCommand(cnf)
		fmt.Println(internalCmd.HelpPage(cnf))
	})
	return cmd
}

func runLint(cmd *cobra.Command, args []string) error {
	result, format, err := lintInput(cmd, args)
	if err != nil {
		// Print operational errors ourselves, since the command silences errors.
		fmt.Fprintln(cmd.ErrOrStderr(), color.RedString(err.Error()))
		return errLintFailed
	}
	return printLintResult(cmd, result, format)
}

func lintInput(cmd *cobra.Command, args []string) (*lint.Result, string, error) {
	useStdin, _ := cmd.Flags().GetBool("stdin")
	format, _ := cmd.Flags().GetString("format")
	if format != "text" && format != "json" {
		return nil, "", fmt.Errorf("invalid --format %q: must be \"text\" or \"json\"", format)
	}

	if !useStdin && len(args) == 0 {
		if stat, err := os.Stdin.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
			useStdin = true
		}
	}

	ctx := cmd.Context()
	if useStdin {
		content, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, format, err
		}
		result, err := lint.LintContent(ctx, string(content))
		return result, format, err
	}

	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	result, _, err := lint.LintDir(ctx, path)
	return result, format, err
}

func printLintResult(cmd *cobra.Command, result *lint.Result, format string) error {
	if format == "json" {
		out := struct {
			Errors   []lint.Issue `json:"errors"`
			Warnings []lint.Issue `json:"warnings"`
		}{Errors: result.Errors, Warnings: result.Warnings}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
		if result.HasErrors() {
			return errLintFailed
		}
		return nil
	}

	w := cmd.ErrOrStderr()
	if result.HasErrors() {
		fmt.Fprintln(w, color.RedString(result.String()))
		return errLintFailed
	}
	if result.HasWarnings() {
		fmt.Fprintln(w, color.YellowString(result.String()))
		return nil
	}
	fmt.Fprintln(w, color.GreenString("✓ The configuration is valid."))
	return nil
}
