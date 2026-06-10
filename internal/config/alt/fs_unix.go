//go:build unix

package alt

import (
	"os"

	"golang.org/x/sys/unix"
)

// isWritableDir reports whether path is a directory the current user can create files in. The
// check is permission-based (no temp file is created), so it is safe to call repeatedly. Both
// write and search (execute) permission are required to create files in a directory.
func isWritableDir(path string) bool {
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return false
	}
	return unix.Access(path, unix.W_OK|unix.X_OK) == nil
}
