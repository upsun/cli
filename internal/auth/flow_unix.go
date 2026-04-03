//go:build !windows

package auth

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mattn/go-isatty"
)

func openBrowser(url string) error {
	// Only attempt to open a browser if stderr is a real terminal.
	// In piped/CI environments, skip the browser open and let the user copy the URL.
	if !isatty.IsTerminal(os.Stderr.Fd()) && !isatty.IsCygwinTerminal(os.Stderr.Fd()) {
		return fmt.Errorf("not a terminal: browser not opened")
	}
	for _, cmd := range []string{"xdg-open", "open"} {
		if err := exec.Command(cmd, url).Start(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no browser opener found")
}
