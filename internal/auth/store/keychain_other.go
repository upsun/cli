//go:build !linux

package store

func secretServiceUnlocked() bool {
	return false
}
