//go:build !windows

package legacy

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// raise ends this process with the signal's default action.
func raise(sig os.Signal) {
	s, ok := sig.(syscall.Signal)
	if !ok {
		os.Exit(1)
	}
	signal.Reset(s)
	_ = syscall.Kill(os.Getpid(), s)
	// The signal may be handled by another thread: this is in case it does not end the process.
	time.Sleep(time.Second)
	os.Exit(128 + int(s))
}
