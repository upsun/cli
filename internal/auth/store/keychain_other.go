//go:build !linux

package store

func secretServiceAvailable() bool {
	return false
}
