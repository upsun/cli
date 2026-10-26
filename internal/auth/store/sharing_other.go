//go:build !windows

package store

func isSharingViolation(error) bool {
	return false
}
