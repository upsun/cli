package config

import "os"

// isWritable checks the read-only attribute, like PHP's is_writable on Windows.
func isWritable(_ string, info os.FileInfo) bool {
	return info.Mode().Perm()&0o200 != 0
}

// ensurePrivateDir does nothing on Windows, where the temporary directory is per user.
func ensurePrivateDir(_ string) error {
	return nil
}
