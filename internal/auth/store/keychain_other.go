//go:build !linux

package store

func secretServiceStatus() (available, unlocked bool) {
	return false, false
}
