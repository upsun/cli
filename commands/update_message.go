package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/upsun/cli/internal"
	"github.com/upsun/cli/internal/config"
)

// updateCheckWait is the maximum time to wait for a background update check before exiting.
const updateCheckWait = time.Second

var (
	// updateCheck is the background update check started by the root command, if any.
	updateCheck *internal.UpdateCheck
	// updateMessage shows a pending update message. It is set by the root command.
	updateMessage *updateNotifier
)

// finishUpdateCheck gives a running update check a chance to save its result,
// and shows an update message if it found a new release.
func finishUpdateCheck() {
	if updateCheck.Wait(updateCheckWait) {
		updateMessage.show(true)
	}
}

// updateNotifier shows a pending update message, at most once.
type updateNotifier struct {
	cnf   *config.Config
	w     io.Writer
	quiet bool
	shown bool

	// These can be replaced in tests.
	pending func() *internal.ReleaseInfo
	method  func() internal.InstallMethod
	mark    func(version string)
}

func newUpdateNotifier(cnf *config.Config, w io.Writer, quiet bool) *updateNotifier {
	return &updateNotifier{
		cnf:     cnf,
		w:       w,
		quiet:   quiet,
		pending: func() *internal.ReleaseInfo { return internal.PendingNotification(cnf, config.Version) },
		method:  func() internal.InstallMethod { return internal.DetectInstallMethod(cnf) },
		mark:    func(version string) { internal.MarkNotified(cnf, version) },
	}
}

// show prints the update message, if one is pending, before the command's
// output or (atExit) after it.
func (n *updateNotifier) show(atExit bool) {
	// In quiet mode the message would be discarded, so it is left for a later run.
	if n == nil || n.shown || n.quiet {
		return
	}
	rel := n.pending()
	if rel == nil {
		return
	}
	n.shown = true
	var msg bytes.Buffer
	if printUpdateMessage(&msg, rel, n.cnf, n.method()) {
		// A blank line separates the message from the command's output.
		if atExit {
			fmt.Fprintf(n.w, "\n%s", msg.String())
		} else {
			fmt.Fprintf(n.w, "%s\n", msg.String())
		}
	}
	n.mark(rel.Version)
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
			return packageUpgradeCommand(method, pkg, privilegeCommand(geteuid() == 0, lookPath))
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

// geteuid and lookPath can be replaced in tests.
var (
	geteuid  = os.Geteuid
	lookPath = exec.LookPath
)

// privilegeCommand returns the command to run another as root: none for root,
// doas if it is installed and sudo is not (e.g. on Alpine), or else sudo.
func privilegeCommand(root bool, lookPath func(string) (string, error)) string {
	if root {
		return ""
	}
	if _, err := lookPath("sudo"); err != nil {
		if _, err := lookPath("doas"); err == nil {
			return "doas"
		}
	}
	return "sudo"
}

// packageUpgradeCommand returns the command to refresh a system package
// manager's index and upgrade a package, running each part with the privilege
// command, if any.
func packageUpgradeCommand(method internal.InstallMethod, pkg, privilege string) string {
	var cmds []string
	switch method {
	case internal.InstallApt:
		cmds = []string{"apt update", "apt install --only-upgrade " + pkg}
	case internal.InstallDnf:
		cmds = []string{"dnf upgrade --refresh " + pkg}
	case internal.InstallYum:
		// Without this, yum uses its cached metadata for hours.
		cmds = []string{"yum clean expire-cache", "yum upgrade " + pkg}
	case internal.InstallApk:
		cmds = []string{"apk add --upgrade --update-cache " + pkg}
	}
	if privilege != "" {
		for i := range cmds {
			cmds[i] = privilege + " " + cmds[i]
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
