package commands

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

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
		// Go error strings are lowercase by convention; capitalize for display.
		fmt.Fprintln(cmd.ErrOrStderr(), color.RedString(capitalizeFirst(err.Error())))
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

	if explicitStdin {
		result, err := lintStdin(cmd)
		return result, format, err
	}

	// An explicit path is linted as given; by default the enclosing repository root is used.
	var root string
	if len(args) == 1 {
		abs, err := filepath.Abs(args[0])
		if err != nil {
			return nil, format, err
		}
		if fi, err := os.Stat(abs); err != nil {
			return nil, format, err
		} else if !fi.IsDir() {
			return nil, format, fmt.Errorf("not a directory: %s", args[0])
		}
		root = abs
	} else {
		root = lint.FindProjectRoot(".")
	}
	if format == "text" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Validating configuration in directory: "+color.CyanString(root))
	}
	result, _, err := lint.CheckDir(root, vendor)
	return result, format, err
}

// capitalizeFirst upper-cases the first rune of s for user-facing display.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// lintStdin reads configuration from standard input and lints it.
func lintStdin(cmd *cobra.Command) (*lint.Result, error) {
	content, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return nil, err
	}
	return lint.CheckContent(string(content))
}

// issuesOrEmpty replaces a nil slice with an empty one, so that the JSON output
// always contains arrays rather than null.
func issuesOrEmpty(issues []lint.Issue) []lint.Issue {
	if issues == nil {
		return []lint.Issue{}
	}
	return issues
}

func printLintResult(cmd *cobra.Command, result *lint.Result, format string) error {
	if format == "json" {
		out := struct {
			Errors   []lint.Issue `json:"errors"`
			Warnings []lint.Issue `json:"warnings"`
		}{Errors: issuesOrEmpty(result.Errors), Warnings: issuesOrEmpty(result.Warnings)}
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
	printIssues(w, color.New(color.FgRed, color.Bold), "Errors", result.Errors)
	printIssues(w, color.New(color.FgYellow, color.Bold), "Warnings", result.Warnings)
	if result.HasErrors() {
		return errLintFailed
	}
	if !result.HasWarnings() {
		fmt.Fprintln(w, color.GreenString("✓")+" The configuration is valid.")
	}
	return nil
}

// printIssues prints a colored heading followed by the issues grouped by file,
// each with its line number and path, and the message below. For example:
//
//	Errors:
//	  .upsun/config.yaml
//	     28  applications.app.authorizations.0.action
//	         authorization type 'env' only allows the action 'view'
//
// It is a no-op when there are no issues.
func printIssues(w io.Writer, heading *color.Color, title string, issues []lint.Issue) {
	if len(issues) == 0 {
		return
	}
	sorted := slices.Clone(issues)
	slices.SortStableFunc(sorted, func(a, b lint.Issue) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Path, b.Path), cmp.Compare(a.Message, b.Message))
	})

	fmt.Fprintln(w, heading.Sprint(title+":"))
	for len(sorted) > 0 {
		n := 1
		for n < len(sorted) && sorted[n].File == sorted[0].File {
			n++
		}
		group := sorted[:n]
		sorted = sorted[n:]
		indent := "  "
		if file := group[0].File; file != "" {
			fmt.Fprintln(w, "  "+color.New(color.Bold).Sprint(file))
			indent = "    "
		}
		// Right-align the line numbers within the file.
		width := 0
		for _, issue := range group {
			if issue.Line > 0 {
				width = max(width, len(strconv.Itoa(issue.Line)))
			}
		}
		msgIndent := indent + "  "
		if width > 0 {
			msgIndent = indent + strings.Repeat(" ", width+2)
		}
		for _, issue := range group {
			var line string
			if width > 0 {
				line = strings.Repeat(" ", width+2)
				if issue.Line > 0 {
					line = color.New(color.Faint).Sprintf("%*d", width, issue.Line) + "  "
				}
			}
			if issue.Path == "" {
				fmt.Fprintln(w, indent+line+issue.Message)
				continue
			}
			fmt.Fprintln(w, indent+line+color.CyanString(issue.Path))
			fmt.Fprintln(w, msgIndent+issue.Message)
		}
	}
}
