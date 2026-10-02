//go:build unix

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// isWritable checks write permission, like PHP's is_writable.
func isWritable(path string, _ os.FileInfo) bool {
	return unix.Access(path, unix.W_OK) == nil
}

// checkPrivateDir checks a directory if others could have created it, i.e. if its parent is world-writable, e.g.
// /tmp. The directory, and any symlink to it, must then be owned by the user (uid), and it is made private.
//
// Otherwise, e.g. in a home directory, it can be owned by another user, as with "sudo -E" or an arbitrary UID.
func checkPrivateDir(path string, uid int) error {
	// G703: the path is the user's own config or temporary directory.
	info, err := os.Lstat(path) //nolint:gosec
	if err != nil {
		return err
	}
	shared, err := hasSharedParent(path)
	if err != nil {
		return err
	}
	if shared && !ownedBy(info, uid) {
		return fmt.Errorf("not owned by the current user: %s", path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		return checkPrivateDir(target, uid)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	if shared && info.Mode().Perm()&0o077 != 0 {
		return os.Chmod(path, 0o700) //nolint:gosec // G703: as above
	}
	return nil
}

func hasSharedParent(path string) (bool, error) {
	info, err := os.Stat(filepath.Dir(path)) //nolint:gosec // G703: as above
	if err != nil {
		return false, err
	}
	return info.Mode().Perm()&0o002 != 0, nil
}

func ownedBy(info os.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

// ensurePrivateDir checks that a directory is private to the current user.
func ensurePrivateDir(path string) error {
	return checkPrivateDir(path, os.Geteuid())
}
