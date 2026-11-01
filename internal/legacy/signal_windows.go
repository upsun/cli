package legacy

import (
	"os"
	"syscall"
)

// raise ends this process with the status that shells report for the signal.
func raise(sig os.Signal) {
	s, ok := sig.(syscall.Signal)
	if !ok {
		os.Exit(1)
	}
	os.Exit(128 + int(s))
}
