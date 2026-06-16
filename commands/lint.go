package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
		Use:           "app:config-validate [path]",
		Short:         "Validate project configuration",
		Aliases:       []string{"lint", "validate"},
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(cmd, args, vendorFromConfig(cnf))
		},
	}
	cmd.Flags().Bool("stdin", false, "Read merged Flex configuration from standard input")
	cmd.Flags().String("format", "text", "Output format: text or json")
	cmd.SetHelpFunc(func(_ *cobra.Command, _ []string) {
		internalCmd := innerAppConfigValidateCommand(cnf)
		fmt.Println(internalCmd.HelpPage(cnf))
	})
	return cmd
}

// vendorFromConfig builds the linter's vendor conventions from the CLI config.
func vendorFromConfig(cnf *config.Config) lint.Vendor {
	return lint.Vendor{
		Flavor:    cnf.Service.ProjectConfigFlavor,
		ConfigDir: cnf.Service.ProjectConfigDir,
		AppFile:   cnf.Service.AppConfigFile,
	}
}

func runLint(cmd *cobra.Command, args []string, vendor lint.Vendor) error {
	result, format, err := lintInput(cmd, args, vendor)
	if err != nil {
		// Print operational errors ourselves, since the command silences errors.
		fmt.Fprintln(cmd.ErrOrStderr(), color.RedString(err.Error()))
		return errLintFailed
	}
	return printLintResult(cmd, result, format)
}

func lintInput(cmd *cobra.Command, args []string, vendor lint.Vendor) (*lint.Result, string, error) {
	explicitStdin, _ := cmd.Flags().GetBool("stdin")
	format, _ := cmd.Flags().GetString("format")
	if format != "text" && format != "json" {
		return nil, "", fmt.Errorf("invalid --format %q: must be \"text\" or \"json\"", format)
	}

	ctx := cmd.Context()
	if explicitStdin {
		result, err := lintStdin(ctx, cmd)
		return result, format, err
	}

	// With no path argument, lint piped stdin if it carries content; otherwise
	// (e.g. a non-interactive shell or CI with no input) fall back to the directory.
	if len(args) == 0 && stdinIsPiped() {
		content, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, format, err
		}
		if strings.TrimSpace(string(content)) != "" {
			result, err := lint.CheckContent(ctx, string(content))
			return result, format, err
		}
	}

	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	root := lint.FindProjectRoot(path)
	if abs, err := filepath.Abs(path); err == nil && abs != root {
		fmt.Fprintln(cmd.ErrOrStderr(), color.New(color.Faint).Sprintf("Linting project root: %s", root))
	}
	result, _, err := lint.CheckDir(ctx, root, vendor)
	return result, format, err
}

// lintStdin reads configuration from standard input and lints it.
func lintStdin(ctx context.Context, cmd *cobra.Command) (*lint.Result, error) {
	content, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return nil, err
	}
	return lint.CheckContent(ctx, string(content))
}

// stdinIsPiped reports whether standard input is a pipe or file rather than a terminal.
func stdinIsPiped() bool {
	stat, err := os.Stdin.Stat()
	return err == nil && (stat.Mode()&os.ModeCharDevice) == 0
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
