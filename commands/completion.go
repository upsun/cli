package commands

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

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
			c := makeLegacyCLIWrapper(cnf, cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin())
			if err := c.Exec(cmd.Context(), append([]string{completeCommandName}, args...)...); err != nil {
				exitWithError(err)
			}
		},
	}
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
