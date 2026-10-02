//go:build !windows

package config

import (
	"os"

	"golang.org/x/sys/unix"
)

// isWritable checks write permission, like PHP's is_writable.
func isWritable(path string, _ os.FileInfo) bool {
	return unix.Access(path, unix.W_OK) == nil
}
