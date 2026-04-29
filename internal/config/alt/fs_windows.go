//go:build windows

package alt

import "os"

// isWritableDir reports whether path is a directory the current user can write to. Windows
// has no equivalent of access(2), so the check is performed by creating and removing a probe
// file.
func isWritableDir(path string) bool {
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return false
	}
	f, err := os.CreateTemp(path, ".platform-alt-write-check-*")
	if err != nil {
		return false
	}
	name := f.Name()
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(name)
		return false
	}
	return os.Remove(name) == nil
}
