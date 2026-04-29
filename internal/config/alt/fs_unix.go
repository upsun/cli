//go:build !windows

package alt

import (
	"os"

	"golang.org/x/sys/unix"
)

// isWritableDir reports whether path is a directory the current user can write to. The check is
// permission-based (no temp file is created), so it is safe to call repeatedly.
func isWritableDir(path string) bool {
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return false
	}
	return unix.Access(path, unix.W_OK) == nil
}
