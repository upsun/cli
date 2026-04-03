//go:build windows

package auth

import "os/exec"

func openBrowser(url string) error {
	return exec.Command("cmd", "/c", "start", url).Start()
}
