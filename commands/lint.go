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
	"github.com/upsun/cli/pkg/lint"
)

// errLintFailed exits with code 1 when the configuration has errors, without another message.
var errLintFailed = &exitError{code: 1}

func newLintCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "app:config-validate [path]",
		Short:   "Validate the configuration files of a project, reporting all errors at once",
		Aliases: []string{"lint", "validate"},
		Annotations: map[string]string{
			argAnnotationPrefix + "path": "The project directory to validate " +
				"(default: the Git repository root, or the current directory)",
		},
		Example: "  # Validate the project configuration files in the current Git repository (or directory)\n" +
			"  " + cnf.Application.Executable + " lint\n\n" +
			"  # Validate the project configuration files in a specific directory\n" +
			"  " + cnf.Application.Executable + " lint path/to/project\n\n" +
			"  # Validate the current Git repository (or directory) and output the results as JSON\n" +
			"  " + cnf.Application.Executable + " lint --format=json",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          runLint,
	}
	cmd.Flags().Bool("stdin", false, "Read merged Flex configuration from standard input")
	cmd.Flags().String("format", "text", `The output format: "text" or "json"`)
	return cmd
}

// lintSupported reports whether the CLI uses the config names that lint looks
// for (.upsun, or .platform with .platform.app.yaml).
func lintSupported(cnf *config.Config) bool {
	switch cnf.Service.ProjectConfigDir {
	case ".upsun":
		return true
	case ".platform":
		return cnf.Service.AppConfigFile == "" || cnf.Service.AppConfigFile == ".platform.app.yaml"
	default:
		return false
	}
}

func runLint(cmd *cobra.Command, args []string) error {
	result, format, err := lintInput(cmd, args)
	if err != nil {
		// Report operational errors as lint errors in JSON, so there is always a document.
		if format == "json" {
			result = &lint.Result{Errors: []lint.Issue{{Message: err.Error()}}}
			return printLintResult(cmd, result, format)
		}
		// Print operational errors ourselves, since the command silences errors.
		// Go error strings are lowercase by convention; capitalize for display.
		fmt.Fprintln(cmd.ErrOrStderr(), color.RedString(capitalizeFirst(err.Error())))
		return errLintFailed
	}
	return printLintResult(cmd, result, format)
}

func lintInput(cmd *cobra.Command, args []string) (*lint.Result, string, error) {
	explicitStdin, _ := cmd.Flags().GetBool("stdin")
	format, _ := cmd.Flags().GetString("format")
	if format != "text" && format != "json" {
		return nil, "", fmt.Errorf("invalid --format %q: must be \"text\" or \"json\"", format)
	}

	if explicitStdin && len(args) > 0 {
		return nil, format, errors.New("--stdin cannot be used with a path")
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
		root = findProjectRoot(".")
	}
	if format == "text" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Validating configuration in directory: "+color.CyanString(root))
	}
	result, err := lint.CheckDir(root)
	return result, format, err
}

// findProjectRoot resolves the project root for a path: the nearest ancestor
// directory that contains a .git entry, or the path itself if none is found.
// This lets the command be run from anywhere inside a repository. The nearest
// (not topmost) .git is used so a stray repository higher up the tree (e.g. a
// dotfiles repo in the home directory) cannot hijack the result.
func findProjectRoot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	for dir := abs; ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		dir = parent
	}
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

	// The report is the command's output, so it is not hidden by --quiet.
	w := cmd.OutOrStdout()
	printIssues(w, color.New(color.FgRed, color.Bold), "Errors", result.Errors)
	printIssues(w, color.New(color.FgYellow, color.Bold), "Warnings", result.Warnings)
	if result.HasErrors() {
		return errLintFailed
	}
	switch n := len(result.Warnings); n {
	case 0:
		fmt.Fprintln(w, color.GreenString("✓")+" The configuration is valid.")
	case 1:
		fmt.Fprintln(w, color.GreenString("✓")+" The configuration is valid, with 1 warning.")
	default:
		fmt.Fprintf(w, "%s The configuration is valid, with %d warnings.\n", color.GreenString("✓"), n)
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
