package commands

import (
	"bytes"
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
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/upsun/cli/internal"
	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/authchannel"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/config/alt"
	"github.com/upsun/cli/internal/legacy"
)

// Execute is the main entrypoint to run the CLI.
func Execute(cnf *config.Config) error {
	ctx := config.ToContext(context.Background(), cnf)
	cmd := newRootCommand(cnf)
	if args, ok, err := expandAbbreviation(cmd, enabledLegacyCommands(cnf, legacy.Commands), os.Args[1:]); err != nil {
		debugLogf("Failed to load the legacy command index: %s", err)
	} else if ok {
		cmd.SetArgs(args)
	}
	err := cmd.ExecuteContext(ctx)
	var ee *exitError
	if errors.As(err, &ee) {
		finishUpdateCheck()
		os.Exit(ee.code)
	}
	if err != nil && !isQuiet() {
		fmt.Fprintln(color.Error, "Error:", err)
	}
	finishUpdateCheck()
	return err
}

// isQuiet reports whether quiet mode is on, which --debug and --verbose override.
func isQuiet() bool {
	return viper.GetBool("quiet") && !viper.GetBool("debug") && !viper.GetBool("verbose")
}

// updateCheckWait is the maximum time to wait for a background update check before exiting.
const updateCheckWait = time.Second

var (
	// updateCheck is the background update check started by the root command, if any.
	updateCheck *internal.UpdateCheck
	// showUpdateMessage shows a pending update message, at most once. It is set by the root command.
	showUpdateMessage func(atExit bool)
)

// finishUpdateCheck gives a running update check a chance to save its result,
// and shows an update message if it found a new release.
func finishUpdateCheck() {
	if updateCheck.Wait(updateCheckWait) && showUpdateMessage != nil {
		showUpdateMessage(true)
	}
}

func newRootCommand(cnf *config.Config) *cobra.Command {
	versionCommand := newVersionCommand(cnf)
	cmd := &cobra.Command{
		Use:                cnf.Application.Executable,
		Short:              cnf.Application.Name,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		SilenceUsage:       true,
		// Errors are printed by Execute, which handles exit codes.
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if isCompletionRequest(cmd) || isInternalCommand(cmd) {
				// Completions and internal commands must be fast and quiet.
				return
			}
			quiet := isQuiet()
			if quiet {
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
			// The check runs in the background and caches its result. It is
			// waited for briefly before exiting.
			updateCheck = internal.StartUpdateCheck(cnf)
			shown := false
			showUpdateMessage = func(atExit bool) {
				// In quiet mode the message would be discarded, so it is left for a later run.
				if shown || quiet {
					return
				}
				rel := internal.PendingNotification(cnf, config.Version)
				if rel == nil {
					return
				}
				shown = true
				var msg bytes.Buffer
				if printUpdateMessage(&msg, rel, cnf, internal.DetectInstallMethod(cnf)) {
					// A blank line separates the message from the command's output.
					if atExit {
						fmt.Fprintf(cmd.ErrOrStderr(), "\n%s", msg.String())
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", msg.String())
					}
				}
				internal.MarkNotified(cnf, rel.Version)
			}
			if viper.GetBool("version") {
				versionCommand.Run(cmd, []string{})
				finishUpdateCheck()
				os.Exit(0)
			}
			// Show any update found by a previous run, before the command's output.
			showUpdateMessage(false)
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
			if isCompletionRequest(cmd) || isInternalCommand(cmd) {
				return
			}
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

	// Add subcommands.
	cmd.AddCommand(
		newCompleteCommand(cnf),
		newConfigInstallCommand(),
		newCompletionCommand(cnf),
		newHelpCommand(cnf),
		useLegacyStyleHelp(cnf, newInitCommand(cnf)),
		newListCommand(cnf),
		versionCommand,
	)
	if lintSupported(cnf) {
		cmd.AddCommand(useLegacyStyleHelp(cnf, newLintCommand(cnf)))
	}
	if cnf.Service.ProjectConfigFlavor == "upsun" {
		cmd.AddCommand(useLegacyStyleHelp(cnf, newProjectConvertCommand(cnf)))
	}
	if cnf.GoAuthEnabled() {
		cmd.AddCommand(newAuthInternalCommand(cnf))
	}
	for _, c := range authCommands(cnf) {
		addLegacyGlobalFlags(c)
		c.PreRun = func(c *cobra.Command, _ []string) { applyLegacyGlobalFlags(c) }
		cmd.AddCommand(useLegacyStyleHelp(cnf, c))
	}

	// Define the help flag before Cobra looks up the command, so that "--help init" does not treat "init" as its value.
	cmd.InitDefaultHelpFlag()

	//nolint:errcheck
	viper.BindPFlags(cmd.PersistentFlags())

	return cmd
}

// isInternalCommand reports whether the command is used internally by the legacy CLI.
func isInternalCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "auth:internal" {
			return true
		}
	}
	return false
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

// printUpdateMessage describes the binary from its embedded config, as the
// binary and its upgrade command are the same with or without CLI_CONFIG_FILE.
// With CLI_CONFIG_FILE, it prints nothing if there is no upgrade command: the
// binary may be a private copy, which the generic instructions do not update.
// It reports whether it printed a message.
func printUpdateMessage(
	w io.Writer, rel *internal.ReleaseInfo, cnf *config.Config, method internal.InstallMethod,
) bool {
	if rel == nil {
		return false
	}
	bin := cnf.Embedded()
	exe, _ := os.Executable()
	upgradeCmd := upgradeCommandFor(bin, method, exe)
	if upgradeCmd == "" && !cnf.IsEmbedded() {
		return false
	}

	fmt.Fprintf(w, "%s %s → %s\n",
		color.YellowString(fmt.Sprintf("A new release of the %s is available:", bin.Application.Name)),
		color.CyanString(config.Version),
		color.CyanString(rel.Version),
	)

	if upgradeCmd != "" {
		fmt.Fprintf(w, "To upgrade, run: %s\n", color.YellowString(upgradeCmd))
	} else if bin.Wrapper.GitHubRepo != "" {
		fmt.Fprintf(
			w,
			"To upgrade, follow the instructions at: https://github.com/%s#upgrade\n",
			bin.Wrapper.GitHubRepo,
		)
	}

	fmt.Fprintln(w, color.YellowString(rel.URL))
	return true
}

// upgradeCommandFor returns the upgrade command for the install method, or an
// empty string when there is no tailored command (the caller then falls back to
// a generic link).
func upgradeCommandFor(cnf *config.Config, method internal.InstallMethod, exe string) string {
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
	case internal.InstallWinget:
		if cnf.Wrapper.WingetPackage != "" {
			return "winget upgrade " + cnf.Wrapper.WingetPackage
		}
	case internal.InstallApt, internal.InstallDnf, internal.InstallYum, internal.InstallApk:
		if pkg := cnf.Wrapper.PackageName; pkg != "" {
			return packageUpgradeCommand(method, pkg, geteuid() == 0)
		}
	case internal.InstallScript:
		// The raw method replaces the binary in INSTALL_DIR; without it, the
		// installer may choose a package manager instead (e.g. apt on Debian).
		if cnf.Wrapper.InstallerURL != "" && exe != "" {
			return "curl -fsSL " + cnf.Wrapper.InstallerURL +
				" | INSTALL_METHOD=raw INSTALL_DIR=" + shellQuote(filepath.Dir(exe)) + " sh"
		}
	}
	return ""
}

// geteuid can be replaced in tests.
var geteuid = os.Geteuid

// packageUpgradeCommand returns the command to refresh a system package
// manager's index and upgrade a package, with sudo unless running as root.
func packageUpgradeCommand(method internal.InstallMethod, pkg string, root bool) string {
	var cmds []string
	switch method {
	case internal.InstallApt:
		cmds = []string{"apt update", "apt install --only-upgrade " + pkg}
	case internal.InstallDnf:
		cmds = []string{"dnf upgrade --refresh " + pkg}
	case internal.InstallYum:
		cmds = []string{"yum upgrade " + pkg}
	case internal.InstallApk:
		cmds = []string{"apk add --upgrade --update-cache " + pkg}
	}
	if !root {
		for i := range cmds {
			cmds[i] = "sudo " + cmds[i]
		}
	}
	return strings.Join(cmds, " && ")
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// shellQuote quotes s for a POSIX shell, if needed.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
		// A process ended by a signal has no exit code: use the status that shells report.
		if ws, ok := execErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			exitCode = 128 + int(ws.Signal())
		}
		debugLogf("%s", err)
		finishUpdateCheck()
		os.Exit(exitCode)
	}
	if !isQuiet() {
		fmt.Fprintln(color.Error, color.RedString(err.Error()))
	}
	finishUpdateCheck()
	os.Exit(1)
}

func makeLegacyCLIWrapper(cnf *config.Config, stdout, stderr io.Writer, stdin io.Reader) *legacy.CLIWrapper {
	c := &legacy.CLIWrapper{
		Config:             cnf,
		Version:            config.Version,
		DebugLogFunc:       debugLogf,
		DisableInteraction: viper.GetBool("no-interaction"),
		Stdout:             stdout,
		Stderr:             stderr,
		Stdin:              stdin,
	}
	if cnf.GoAuthEnabled() {
		authStderr := stderr
		if authStderr == nil {
			authStderr = io.Discard
		}
		newManager := func() (*auth.Manager, error) { return newAuthManager(cnf, authStderr) }
		c.PrepareCmd = authchannel.Prepare(cnf, newManager, debugLogf)
	}
	return c
}
