package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fatih/color"
	"github.com/platformsh/platformify/commands"
	"github.com/platformsh/platformify/vendorization"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/upsun/cli/internal"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/config/alt"
	"github.com/upsun/cli/internal/legacy"
)

// Execute is the main entrypoint to run the CLI.
func Execute(cnf *config.Config) error {
	assets := &vendorization.VendorAssets{
		Use:          "project:init",
		Binary:       cnf.Application.Executable,
		ConfigFlavor: cnf.Service.ProjectConfigFlavor,
		EnvPrefix:    strings.TrimSuffix(cnf.Service.EnvPrefix, "_"),
		ServiceName:  cnf.Service.Name,
		DocsBaseURL:  cnf.Service.DocsURL,
	}

	ctx := vendorization.WithVendorAssets(config.ToContext(context.Background(), cnf), assets)
	return newRootCommand(cnf, assets).ExecuteContext(ctx)
}

func newRootCommand(cnf *config.Config, assets *vendorization.VendorAssets) *cobra.Command {
	versionCommand := newVersionCommand(cnf)
	cmd := &cobra.Command{
		Use:                cnf.Application.Executable,
		Short:              cnf.Application.Name,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		SilenceUsage:       true,
		SilenceErrors:      false,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if viper.GetBool("quiet") && !viper.GetBool("debug") && !viper.GetBool("verbose") {
				viper.Set("no-interaction", true)
				cmd.SetErr(io.Discard)
			} else {
				// Ensure the command's output writers can handle colors.
				if cmd.OutOrStdout() == os.Stdout {
					cmd.SetOut(color.Output)
				}
				if cmd.ErrOrStderr() == os.Stderr {
					cmd.SetErr(color.Error)
				}
			}
			if viper.GetBool("yes") {
				viper.Set("no-interaction", true)
			}
			if viper.GetBool("version") {
				versionCommand.Run(cmd, []string{})
				os.Exit(0)
			}
			if cnf.Wrapper.GitHubRepo != "" {
				// Show any update found by a previous run, before the command's
				// output. The check itself runs in the background (below) and
				// caches its result for the next invocation.
				if rel := internal.PendingNotification(cnf, config.Version); rel != nil {
					printUpdateMessage(cmd.ErrOrStderr(), rel, cnf)
					internal.MarkNotified(cnf)
				}
				go func() {
					//nolint:errcheck // a failed update check should not affect the command
					internal.CheckForUpdate(cnf, config.Version)
				}()
			}
			if alt.ShouldUpdate(cnf) {
				go func() {
					if err := alt.Update(cmd.Context(), cnf, debugLogf); err != nil {
						cmd.PrintErrln("Error updating config:", color.RedString(err.Error()))
					}
				}()
			}
		},
		Run: func(cmd *cobra.Command, _ []string) {
			c := makeLegacyCLIWrapper(cnf, cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin())
			if err := c.Exec(cmd.Context(), os.Args[1:]...); err != nil {
				exitWithError(err)
			}
		},
		PersistentPostRun: func(cmd *cobra.Command, _ []string) {
			checkShellConfigLeftovers(cmd.ErrOrStderr(), cnf)
		},
	}

	cmd.SetHelpFunc(func(innerCmd *cobra.Command, args []string) {
		if innerCmd.Use != cmd.Use {
			// For real (Cobra) commands, print the usage string.
			innerCmd.Print(innerCmd.UsageString())
			return
		}

		// Others will be passed to the legacy CLI's help command.
		if !slices.Contains(args, "--help") && !slices.Contains(args, "-h") {
			args = append([]string{"help"}, args...)
		}
		if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
			args = []string{"help"}
		}

		c := makeLegacyCLIWrapper(cnf, cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin())
		if err := c.Exec(cmd.Context(), args...); err != nil {
			exitWithError(err)
		}
	})

	cmd.PersistentFlags().BoolP("version", "V", false, fmt.Sprintf("Displays the %s version", cnf.Application.Name))
	cmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	cmd.PersistentFlags().Bool("no-interaction", false, "Enable non-interactive mode")
	cmd.PersistentFlags().BoolP("yes", "y", false, "Answer yes to all confirmation questions; implies --no-interaction")
	cmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	cmd.PersistentFlags().BoolP("quiet", "q", false,
		"Suppress any messages and errors (stderr), while continuing to display necessary output (stdout)."+
			" This implies --no-interaction. Ignored in verbose mode.",
	)

	validateCmd := commands.NewValidateCommand(assets)
	validateCmd.Use = "app:config-validate"
	validateCmd.Aliases = []string{"validate", "lint"}
	validateCmd.SetHelpFunc(func(_ *cobra.Command, _ []string) {
		internalCmd := innerAppConfigValidateCommand(cnf)
		fmt.Println(internalCmd.HelpPage(cnf))
	})

	// Add subcommands.
	cmd.AddCommand(
		newConfigInstallCommand(),
		newCompletionCommand(cnf),
		newHelpCommand(cnf),
		newInitCommand(cnf, assets),
		newListCommand(cnf),
		validateCmd,
		versionCommand,
	)
	if cnf.Service.ProjectConfigFlavor == "upsun" {
		cmd.AddCommand(newProjectConvertCommand(cnf))
	}

	//nolint:errcheck
	viper.BindPFlags(cmd.PersistentFlags())

	return cmd
}

// checkShellConfigLeftovers checks .zshrc and .bashrc for any leftovers from the legacy CLI
func checkShellConfigLeftovers(w io.Writer, cnf *config.Config) {
	start := fmt.Sprintf("# BEGIN SNIPPET: %s configuration", cnf.Application.Name)
	end := "# END SNIPPET"
	shellConfigSnippet := regexp.MustCompile(regexp.QuoteMeta(start) + "(?s).+?" + regexp.QuoteMeta(end))

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	shellConfigFiles := []string{
		filepath.Join(homeDir, ".zshrc"),
		filepath.Join(homeDir, ".bashrc"),
	}

	for _, shellConfigFile := range shellConfigFiles {
		if _, err := os.Stat(shellConfigFile); err != nil {
			continue
		}

		shellConfig, err := os.ReadFile(shellConfigFile)
		if err != nil {
			continue
		}

		if shellConfigSnippet.Match(shellConfig) {
			fmt.Fprintf(w, "%s Your %s file contains code that is no longer needed for the New %s\n",
				color.YellowString("Notice:"),
				shellConfigFile,
				cnf.Application.Name,
			)
			fmt.Fprintf(w, "%s %s\n", color.YellowString("Please remove the following lines from:"), shellConfigFile)
			fmt.Fprintf(w, "\t%s\n", strings.ReplaceAll(string(shellConfigSnippet.Find(shellConfig)), "\n", "\n\t"))
		}
	}
}

func printUpdateMessage(w io.Writer, newRelease *internal.ReleaseInfo, cnf *config.Config) {
	if newRelease == nil {
		return
	}

	fmt.Fprintf(w, "\n%s %s → %s\n",
		color.YellowString(fmt.Sprintf("A new release of the %s is available:", cnf.Application.Name)),
		color.CyanString(config.Version),
		color.CyanString(newRelease.Version),
	)

	if cmd := upgradeCommand(cnf); cmd != "" {
		fmt.Fprintf(w, "To upgrade, run: %s\n", color.YellowString(cmd))
	} else if cnf.Wrapper.GitHubRepo != "" {
		fmt.Fprintf(
			w,
			"To upgrade, follow the instructions at: https://github.com/%s#upgrade\n",
			cnf.Wrapper.GitHubRepo,
		)
	}

	fmt.Fprintf(w, "%s\n\n", color.YellowString(newRelease.URL))
}

// upgradeCommand returns the upgrade command for the detected install method, or
// an empty string when there is no tailored command (the caller then falls back
// to a generic link).
func upgradeCommand(cnf *config.Config) string {
	return upgradeCommandFor(cnf, internal.DetectInstallMethod(cnf))
}

func upgradeCommandFor(cnf *config.Config, method internal.InstallMethod) string {
	switch method {
	case internal.InstallHomebrew:
		if cnf.Wrapper.HomebrewTap != "" {
			return "brew update && brew upgrade " + cnf.Wrapper.HomebrewTap
		}
	case internal.InstallScoop:
		return "scoop update " + cnf.Application.Executable
	case internal.InstallNpm:
		if cnf.Wrapper.NpmPackage != "" {
			return "npm install -g " + cnf.Wrapper.NpmPackage + "@latest"
		}
	case internal.InstallScript:
		if cnf.Wrapper.InstallerURL != "" {
			return "curl -fsSL " + cnf.Wrapper.InstallerURL + " | bash"
		}
	}
	return ""
}

func debugLogf(format string, v ...any) {
	if !viper.GetBool("debug") {
		return
	}

	prefix := color.New(color.ReverseVideo).Sprintf("DEBUG")
	fmt.Fprintf(color.Error, prefix+" "+strings.TrimSpace(format)+"\n", v...)
}

func exitWithError(err error) {
	var execErr *exec.ExitError
	if errors.As(err, &execErr) {
		exitCode := execErr.ExitCode()
		debugLogf(err.Error())
		os.Exit(exitCode)
	}
	if !viper.GetBool("quiet") {
		fmt.Fprintln(color.Error, color.RedString(err.Error()))
	}
	os.Exit(1)
}

func makeLegacyCLIWrapper(cnf *config.Config, stdout, stderr io.Writer, stdin io.Reader) *legacy.CLIWrapper {
	return &legacy.CLIWrapper{
		Config:             cnf,
		Version:            config.Version,
		DebugLogFunc:       debugLogf,
		DisableInteraction: viper.GetBool("no-interaction"),
		Stdout:             stdout,
		Stderr:             stderr,
		Stdin:              stdin,
	}
}
