package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"

	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
)

// authCommands returns the native auth commands that are enabled, which are listed alongside the legacy CLI's.
// There are none unless Go auth is enabled.
func authCommands(cnf *config.Config) []*cobra.Command {
	if !cnf.GoAuthEnabled() {
		return nil
	}
	all := []*cobra.Command{
		newAPITokenLoginCommand(cnf),
		newAuthTokenCommand(cnf),
		newBrowserLoginCommand(cnf),
		newLogoutCommand(cnf),
	}
	return slices.DeleteFunc(all, func(c *cobra.Command) bool {
		return slices.Contains(cnf.Application.DisabledCommands, c.Name()) ||
			slices.Contains(cnf.Application.WrappedDisabledCommands, c.Name())
	})
}

// addLegacyGlobalFlags accepts the legacy CLI's global options that the root command does not define.
func addLegacyGlobalFlags(c *cobra.Command) {
	c.Flags().BoolP("no", "n", false, "Answer \"no\" to confirmation questions; disable interaction")
	c.Flags().Bool("ansi", false, "Force ANSI output")
	c.Flags().Bool("no-ansi", false, "Disable ANSI output")
	for _, name := range []string{"no", "ansi", "no-ansi"} {
		_ = c.Flags().MarkHidden(name)
	}
}

// applyLegacyGlobalFlags applies the options added by addLegacyGlobalFlags.
func applyLegacyGlobalFlags(c *cobra.Command) {
	if no, _ := c.Flags().GetBool("no"); no {
		viper.Set("no", true)
		viper.Set("no-interaction", true)
	}
	if ansi, _ := c.Flags().GetBool("ansi"); ansi {
		color.NoColor = false
	}
	if noANSI, _ := c.Flags().GetBool("no-ansi"); noANSI {
		color.NoColor = true
	}
}

// newAuthManager creates the auth manager, which migrates the legacy CLI's sessions on first use.
func newAuthManager(cnf *config.Config, stderr io.Writer) (*auth.Manager, error) {
	m, err := auth.NewManager(cnf, stderr)
	if err != nil {
		return nil, err
	}
	m.Migrator = &auth.Migrator{
		Export: func(ctx context.Context, del bool) ([]byte, error) {
			var stdout, errOut bytes.Buffer
			c := makeLegacyCLIWrapper(cnf, &stdout, &errOut, nil)
			c.DisableInteraction = true
			args := []string{"auth:export-sessions"}
			if del {
				args = append(args, "--delete")
			}
			if err := c.Exec(ctx, args...); err != nil {
				return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(errOut.String()))
			}
			return stdout.Bytes(), nil
		},
		DebugLog: debugLogf,
	}
	m.OnLoggedOut = func(id string) error {
		return clearLegacySessionFiles(cnf, m.Settings.SessionID, []string{id}, false)
	}
	return m, nil
}

// runLegacyAuthHook runs a hidden legacy CLI command that completes a login (SSH certificates and config).
func runLegacyAuthHook(cmd *cobra.Command, cnf *config.Config, args ...string) error {
	c := makeLegacyCLIWrapper(cnf, cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin())
	return c.Exec(cmd.Context(), args...)
}

// isInteractive reports whether questions may be asked, in the same way as the legacy CLI (Symfony Console).
func isInteractive(cmd *cobra.Command) bool {
	if viper.GetBool("no-interaction") {
		return false
	}
	if _, ok := os.LookupEnv("SHELL_INTERACTIVE"); ok {
		return true
	}
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// stdinReaders shares one buffered reader per input, so that consecutive questions do not lose buffered input.
var (
	stdinReaders   = map[io.Reader]*bufio.Reader{}
	stdinReadersMu sync.Mutex
)

func readLine(r io.Reader) (string, error) {
	stdinReadersMu.Lock()
	br, ok := stdinReaders[r]
	if !ok {
		br = bufio.NewReader(r)
		stdinReaders[r] = br
	}
	stdinReadersMu.Unlock()
	line, err := br.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// confirm asks a yes/no question.
func confirm(cmd *cobra.Command, question string, def bool) (bool, error) {
	if viper.GetBool("yes") {
		return true, nil
	}
	if viper.GetBool("no") {
		return false, nil
	}
	if !isInteractive(cmd) {
		return def, nil
	}
	hint := "Y/n"
	if !def {
		hint = "y/N"
	}
	for {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s [%s] ", question, hint)
		answer, err := readLine(cmd.InOrStdin())
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

// readSecret reads a line without echoing it, if the input is a terminal.
func readSecret(cmd *cobra.Command) (string, error) {
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		return string(b), err
	}
	return readLine(cmd.InOrStdin())
}

// exitError ends a command with an exit code, after its message has been printed.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

// nonInteractiveAuthHelp matches the legacy CLI's Login::getNonInteractiveAuthHelp().
func nonInteractiveAuthHelp(cnf *config.Config) string {
	return fmt.Sprintf("To authenticate non-interactively, configure an API token using the %s environment variable.",
		color.YellowString(cnf.Application.EnvPrefix+"TOKEN"))
}

// sessionAdvice returns lines about the current session, if there are several, as in the legacy CLI.
func sessionAdvice(ctx context.Context, cnf *config.Config, m *auth.Manager, changeVerb string) []string {
	ids, err := m.SessionIDs(ctx)
	if err != nil {
		debugLogf("Failed to list sessions: %s", err)
	}
	if m.Settings.SessionID == "default" && len(ids) <= 1 {
		return nil
	}
	lines := []string{fmt.Sprintf("The current session ID is: %s", color.GreenString(m.Settings.SessionID))}
	if !m.Settings.SessionIDFromEnv {
		lines = append(lines, fmt.Sprintf("%s: %s", changeVerb,
			color.GreenString(cnf.Application.Executable+" session:switch")))
	}
	return lines
}

// handleLoginRequired offers a browser login if possible, as the legacy CLI's AutoLoginListener did.
// It returns nil if the user logged in, or an *exitError (code 3) after printing why login is required.
func handleLoginRequired(cmd *cobra.Command, cnf *config.Config, m *auth.Manager, lerr *auth.LoginRequiredError) error {
	stderr := cmd.ErrOrStderr()
	if lerr.Notice != "" {
		fmt.Fprintln(stderr, color.YellowString(lerr.Notice))
		fmt.Fprintln(stderr)
	}
	if isInteractive(cmd) && canOpenURLs("") {
		fmt.Fprintln(stderr, lerr.Message())
		fmt.Fprintln(stderr)
		if advice := sessionAdvice(cmd.Context(), cnf, m, "To switch sessions, run"); advice != nil {
			fmt.Fprintln(stderr, strings.Join(advice, "\n"))
			fmt.Fprintln(stderr)
		}
		ok, err := confirm(cmd, "Log in via a browser?", true)
		if err != nil {
			return err
		}
		if ok {
			fmt.Fprintln(stderr)
			opts := &browserLoginOptions{methods: lerr.AuthMethods, maxAge: lerr.MaxAge}
			err := runBrowserLogin(cmd, cnf, m, opts)
			fmt.Fprintln(stderr)
			if err == nil {
				return nil
			}
			var ee *exitError
			if !errors.As(err, &ee) {
				return err
			}
		}
	}
	fmt.Fprintln(stderr, loginRequiredMessage(cnf, lerr))
	return &exitError{code: auth.ExitCodeLoginRequired}
}

// loginRequiredMessage matches the legacy CLI's LoginRequiredEvent::getExtendedMessage().
func loginRequiredMessage(cnf *config.Config, lerr *auth.LoginRequiredError) string {
	msg := lerr.Message()
	if lerr.HasAPIToken {
		if len(lerr.AuthMethods) == 1 && lerr.AuthMethods[0] == "mfa" {
			msg += "\n\nThe API token may need to be re-created after enabling MFA."
		}
		return msg
	}
	loginCmd := "login"
	if len(lerr.AuthMethods) > 0 {
		loginCmd += " --method " + shellQuote(strings.Join(lerr.AuthMethods, ","))
	}
	if lerr.MaxAge != nil {
		loginCmd += fmt.Sprintf(" --max-age %d", *lerr.MaxAge)
	}
	return fmt.Sprintf("%s\n\nPlease log in by running:\n    %s %s", msg, cnf.Application.Executable, loginCmd)
}

// withLogin runs fn, and if it reports that login is required, offers a login and runs it again.
func withLogin(cmd *cobra.Command, cnf *config.Config, m *auth.Manager, fn func() error) error {
	err := fn()
	lerr, ok := auth.AsLoginRequired(err)
	if !ok {
		return err
	}
	if err := handleLoginRequired(cmd, cnf, m, lerr); err != nil {
		return err
	}
	return fn()
}

// hasDisplay matches the legacy CLI's Url::hasDisplay(), and also counts WSL, where a Windows browser can be used.
func hasDisplay() bool {
	if d := os.Getenv("DISPLAY"); d != "" {
		return d != "none"
	}
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin" || isWSL()
}

// isWSL reports whether this is Linux in the Windows Subsystem for Linux.
func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// wslBrowserCommand returns a command to open URLs in a Windows browser from WSL, or nil if none is found.
func wslBrowserCommand() []string {
	if p, err := exec.LookPath("wslview"); err == nil {
		return []string{p}
	}
	// Windows paths can be left out of PATH (appendWindowsPath=false in wsl.conf).
	for _, p := range []string{"rundll32.exe", "/mnt/c/Windows/System32/rundll32.exe"} {
		if p, err := exec.LookPath(p); err == nil {
			return []string{p, "url.dll,FileProtocolHandler"}
		}
	}
	return nil
}

// browserCommand returns the command to open URLs, or nil if none should be used.
func browserCommand(browserOption string) []string {
	switch {
	case browserOption == "0":
		return nil
	case strings.TrimSpace(browserOption) != "":
		fields := strings.Fields(browserOption)
		if _, err := exec.LookPath(fields[0]); err != nil {
			return nil
		}
		return fields
	case runtime.GOOS == "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler"}
	case runtime.GOOS == "darwin":
		return []string{"open"}
	case isWSL():
		if c := wslBrowserCommand(); c != nil {
			return c
		}
	}
	for _, b := range []string{"xdg-open", "gnome-open"} {
		if _, err := exec.LookPath(b); err == nil {
			return []string{b}
		}
	}
	return nil
}

func canOpenURLs(browserOption string) bool {
	return hasDisplay() && browserCommand(browserOption) != nil
}

// openURL opens a URL in a browser, and reports whether it did.
func openURL(url, browserOption string) bool {
	if !hasDisplay() {
		debugLogf("Not opening URL (no display found)")
		return false
	}
	args := browserCommand(browserOption)
	if args == nil {
		return false
	}
	//nolint:gosec // the browser is chosen by the user or the OS
	return exec.Command(args[0], append(args[1:], url)...).Run() == nil
}

func newAuthTokenCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "auth:token",
		Short:  "Obtain an OAuth 2 access token for API requests",
		Hidden: true,
		Args:   cobra.NoArgs,
		Long: "This command prints a valid OAuth 2 access token to stdout. It can be used to make API requests via " +
			"standard Bearer authentication (RFC 6750).\n\n" +
			color.YellowString("Warning: access tokens must be kept secret.") + "\n\n" +
			"Using this command is not generally recommended, as it increases the chance of the token being leaked. " +
			"Take care not to expose the token in a shared program or system, or to send the token to the wrong " +
			"API domain.",
		Example: fmt.Sprintf("  # Print the payload for JWT-formatted tokens\n"+
			"  %[1]s auth:token -W | cut -d. -f2 | base64 -d\n\n"+
			"  # Use the token in a curl command\n  curl -H\"$(%[1]s auth:token -HW)\" %[2]s/users/me",
			cnf.Application.Executable, strings.TrimRight(cnf.API.BaseURL, "/")),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if noWarn, _ := cmd.Flags().GetBool("no-warn"); !noWarn {
				fmt.Fprintln(cmd.ErrOrStderr(), color.YellowString("Warning: keep access tokens secret."))
			}
			m, err := newAuthManager(cnf, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			var tok *auth.Token
			if err := withLogin(cmd, cnf, m, func() (err error) {
				tok, err = m.Token(cmd.Context(), "")
				return err
			}); err != nil {
				return err
			}
			out := tok.AccessToken
			if header, _ := cmd.Flags().GetBool("header"); header {
				out = "Authorization: Bearer " + out
			}
			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().BoolP("header", "H", false, `Prefix the token with "Authorization: Bearer " to make an RFC 6750 header`)
	cmd.Flags().BoolP("no-warn", "W", false, "Suppress the warning that is printed by default to stderr."+
		" This option is preferred over redirecting stderr, as that would hide other potentially useful messages.")
	return cmd
}

// newAuthInternalCommand is used by the legacy CLI to get tokens and auth state. It never prompts.
//
// Its stdout is only JSON. Exit code 3 means login is required, with the reason as JSON on the last line of stderr.
func newAuthInternalCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "auth:internal",
		Short:  "Internal: provide tokens and auth state to the legacy CLI",
		Hidden: true,
	}
	run := func(fn func(cmd *cobra.Command, m *auth.Manager) (any, error)) func(cmd *cobra.Command, _ []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			m, err := newAuthManager(cnf, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			result, err := fn(cmd, m)
			if lerr, ok := auth.AsLoginRequired(err); ok {
				b, _ := json.Marshal(lerr)
				fmt.Fprintln(cmd.ErrOrStderr(), string(b))
				return &exitError{code: auth.ExitCodeLoginRequired}
			}
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
	}
	tokenCmd := &cobra.Command{
		Use:  "token",
		Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, m *auth.Manager) (any, error) {
			var rejected string
			// The rejected token is read from stdin, to keep it out of process listings.
			if r, _ := cmd.Flags().GetBool("rejected"); r {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return nil, err
				}
				rejected = strings.TrimSpace(string(b))
			}
			return m.Token(cmd.Context(), rejected)
		}),
	}
	tokenCmd.Flags().Bool("rejected", false, "Read an access token that was rejected by the API from stdin")
	statusCmd := &cobra.Command{
		Use:  "status",
		Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, m *auth.Manager) (any, error) {
			return m.Status(cmd.Context())
		}),
	}
	cmd.AddCommand(tokenCmd, statusCmd)
	return cmd
}
