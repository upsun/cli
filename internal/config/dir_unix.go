//go:build !windows

package config

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// isWritable checks write permission, like PHP's is_writable.
func isWritable(path string, _ os.FileInfo) bool {
	return unix.Access(path, unix.W_OK) == nil
}

// checkPrivateDir checks that a directory, and any symlink to it, is owned by the user (uid), and makes it private.
//
// This prevents another user from controlling the directory, e.g. if it is in a shared /tmp.
func checkPrivateDir(path string, uid int) error {
	// G703: the path is the user's own config or temporary directory.
	info, err := os.Lstat(path) //nolint:gosec
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if !ownedBy(info, uid) {
			return fmt.Errorf("the symlink is not owned by the current user: %s", path)
		}
		if info, err = os.Stat(path); err != nil { //nolint:gosec // G703: as above
			return err
		}
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	if !ownedBy(info, uid) {
		return fmt.Errorf("the directory is not owned by the current user: %s", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return os.Chmod(path, 0o700) //nolint:gosec // G703: as above
	}
	return nil
}

func ownedBy(info os.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

// ensurePrivateDir checks that a directory is private to the current user.
func ensurePrivateDir(path string) error {
	return checkPrivateDir(path, os.Geteuid())
}
