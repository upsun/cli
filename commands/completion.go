package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/file"
)

const (
	shellBash = "bash"
	shellZsh  = "zsh"
)

func newCompletionCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "completion",
		Short:         "Print the completion script for your shell",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			shell := ""
			if len(args) > 0 {
				shell = args[0]
			}
			script, err := generateCompletionScript(cmd.Context(), cnf, shell, cmd.ErrOrStderr(), cmd.InOrStdin())
			if err != nil {
				exitWithError(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), script)
		},
	}
	cmd.AddCommand(newCompletionInstallCommand(cnf))
	return cmd
}

// generateCompletionScript runs the legacy CLI's completion command and
// rewrites references to the Phar so the script invokes the wrapper binary.
func generateCompletionScript(
	ctx context.Context, cnf *config.Config, shell string, stderr io.Writer, stdin io.Reader,
) (string, error) {
	completionArgs := []string{"completion"}
	if shell != "" {
		completionArgs = append(completionArgs, shell)
	}
	var b bytes.Buffer
	c := makeLegacyCLIWrapper(cnf, &b, stderr, stdin)
	if err := c.Exec(ctx, completionArgs...); err != nil {
		return "", err
	}
	pharPath, err := c.PharPath()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(
		strings.ReplaceAll(
			b.String(),
			pharPath,
			cnf.Application.Executable,
		),
		filepath.Base(pharPath),
		cnf.Application.Executable,
	), nil
}

func newCompletionInstallCommand(cnf *config.Config) *cobra.Command {
	var (
		shellFlag string
		pathFlag  string
		printPath bool
	)
	cmd := &cobra.Command{
		Use:   "install [shell]",
		Short: "Install the shell completion script",
		Long: `Install the shell completion script to the standard location for the detected shell.

Supported shells: bash, zsh.

The shell is detected from the SHELL environment variable. Override it with the
--shell flag or by passing a positional argument.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := shellFlag
			if shell == "" && len(args) > 0 {
				shell = args[0]
			}
			if shell == "" {
				shell = detectShell()
			}
			if shell == "" {
				return fmt.Errorf("could not detect shell from $SHELL; pass the shell as an argument or via --shell")
			}
			switch shell {
			case shellBash, shellZsh:
			default:
				return fmt.Errorf("unsupported shell %q (supported: bash, zsh)", shell)
			}

			target := pathFlag
			if target == "" {
				t, err := defaultCompletionPath(cnf.Application.Executable, shell)
				if err != nil {
					return err
				}
				target = t
			}

			if printPath {
				fmt.Fprintln(cmd.OutOrStdout(), target)
				return nil
			}

			script, err := generateCompletionScript(cmd.Context(), cnf, shell, cmd.ErrOrStderr(), cmd.InOrStdin())
			if err != nil {
				return err
			}

			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("failed to create %s: %w", filepath.Dir(target), err)
			}
			// Completion scripts must be world-readable so other users on multi-user
			// systems can source them; they contain no secrets.
			if err := file.Write(target, []byte(script), 0o644); err != nil {
				return fmt.Errorf("failed to write %s: %w", target, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s completion at %s\n", shell, target)
			if note := postInstallNote(shell, target); note != "" {
				fmt.Fprintln(cmd.OutOrStdout(), note)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&shellFlag, "shell", "", "Shell to install completion for (bash or zsh)")
	cmd.Flags().StringVar(&pathFlag, "path", "", "Path to write the completion file (overrides the default)")
	cmd.Flags().BoolVar(&printPath, "print-path", false, "Print the target path without installing")
	return cmd
}

// detectShell returns "bash" or "zsh" if $SHELL points at one of them, or "" otherwise.
func detectShell() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return ""
	}
	switch filepath.Base(sh) {
	case shellBash:
		return shellBash
	case shellZsh:
		return shellZsh
	}
	return ""
}

// defaultCompletionPath returns the standard install location for the given shell,
// matching what the deb/rpm/apk packages and Homebrew formula already use.
func defaultCompletionPath(binary, shell string) (string, error) {
	const (
		systemBashDir = "/etc/bash_completion.d"
		systemZshDir  = "/usr/local/share/zsh/site-functions"
	)
	isRoot := os.Geteuid() == 0
	switch shell {
	case shellBash:
		if isRoot {
			return filepath.Join(systemBashDir, binary), nil
		}
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("could not determine home directory: %w", err)
			}
			dataHome = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(dataHome, "bash-completion", "completions", binary), nil
	case shellZsh:
		if isRoot {
			return filepath.Join(systemZshDir, "_"+binary), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not determine home directory: %w", err)
		}
		return filepath.Join(home, ".zsh", "completions", "_"+binary), nil
	}
	return "", fmt.Errorf("unsupported shell %q", shell)
}

// postInstallNote returns shell-specific instructions printed after a successful install.
func postInstallNote(shell, target string) string {
	switch shell {
	case shellZsh:
		dir := filepath.Dir(target)
		return fmt.Sprintf("\nIf %[1]s is not already in your $fpath, add this to your ~/.zshrc:\n\n"+
			"  fpath+=(%[1]s)\n  autoload -U compinit && compinit\n\n"+
			"Then restart your shell, or run: exec zsh", dir)
	case shellBash:
		return "\nRestart your shell or run: source " + target
	}
	return ""
}
