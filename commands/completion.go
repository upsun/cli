package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/upsun/cli/internal/config"
)

// completeCommandName is the hidden legacy (Symfony Console) command that the
// generated completion scripts call to fetch suggestions.
const completeCommandName = "_complete"

// shellOptionReplacer rewrites the shell option of the generated completion
// scripts from Symfony's glued short form (-szsh) to the long form
// (--shell=zsh), which no argument parser can misread as a flag bundle.
var shellOptionReplacer = strings.NewReplacer(
	"-szsh", "--shell=zsh",
	"-sbash", "--shell=bash",
	"-sfish", "--shell=fish",
)

func newCompletionCommand(cnf *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:           "completion",
		Short:         "Print the completion script for your shell",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			// The legacy 5.x CLI uses Symfony's native completion command.
			completionArgs := []string{"completion"}
			if len(args) > 0 {
				completionArgs = append(completionArgs, args[0])
			}
			var b bytes.Buffer
			c := makeLegacyCLIWrapper(cnf, &b, cmd.ErrOrStderr(), cmd.InOrStdin())

			if err := c.Exec(cmd.Context(), completionArgs...); err != nil {
				exitWithError(err)
			}

			pharPath, err := c.PharPath()
			if err != nil {
				exitWithError(err)
			}

			completions := strings.ReplaceAll(
				strings.ReplaceAll(
					b.String(),
					pharPath,
					cnf.Application.Executable,
				),
				filepath.Base(pharPath),
				cnf.Application.Executable,
			)
			fmt.Fprintln(cmd.OutOrStdout(), shellOptionReplacer.Replace(completions))
		},
	}
}

// newCompleteCommand proxies the hidden _complete command of the legacy CLI,
// which the completion scripts call to fetch suggestions.
//
// It only exists to keep Cobra from parsing those arguments. The scripts pass
// the shell as a glued short option (-szsh, -sbash, -sfish), which Cobra
// splits into single-letter flags; as every supported shell name contains an
// "h", that always produced a -h flag and the CLI printed help instead of
// completions.
func newCompleteCommand(cnf *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:                completeCommandName,
		Short:              "Internal command to provide shell completion suggestions",
		Hidden:             true,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		Run: func(cmd *cobra.Command, args []string) {
			err := holdStderr(cmd.ErrOrStderr(), func(stderr io.Writer) error {
				c := makeLegacyCLIWrapper(cnf, cmd.OutOrStdout(), stderr, cmd.InOrStdin())
				return c.Exec(cmd.Context(), append([]string{completeCommandName}, args...)...)
			})
			if err != nil {
				exitSilently(err)
			}
		},
	}
}

// holdStderr runs fn with a buffered stderr, which is only written out if fn
// fails or debug mode is on. As _complete does not parse flags, debug mode
// can only be enabled with the <PREFIX>DEBUG environment variable. The bash
// completion script captures stderr along with stdout and, on success, offers
// all of it as suggestions, so a warning (e.g. from PHP) must not get through;
// on failure it prints the output as a diagnostic.
func holdStderr(stderr io.Writer, fn func(stderr io.Writer) error) error {
	var b bytes.Buffer
	err := fn(&b)
	if err != nil || viper.GetBool("debug") {
		_, _ = stderr.Write(b.Bytes())
	}

	return err
}

// isCompletionRequest reports whether the command was run by a completion
// script rather than by a user. Those runs must stay silent: the bash script
// captures stderr along with stdout, so any extra message ends up in the
// suggestions.
func isCompletionRequest(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case completeCommandName, cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}

	return false
}

// exitSilently ends a completion request, reporting the exit code of the
// legacy CLI without writing anything. Unlike exitWithError it keeps quiet
// even for a failure that is not an exit code: the bash completion script
// captures stderr along with stdout, so the message would be offered as a
// suggestion.
func exitSilently(err error) {
	debugLogf("%s failed: %s", completeCommandName, err)

	var execErr *exec.ExitError
	if errors.As(err, &execErr) {
		os.Exit(execErr.ExitCode())
	}

	os.Exit(1)
}
