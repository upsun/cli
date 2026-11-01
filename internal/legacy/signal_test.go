package legacy

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEndedBySignal(t *testing.T) {
	cases := []struct {
		sig      os.Signal
		exitCode int
		want     bool
	}{
		{nil, 0, false},
		{nil, 130, false},
		{os.Interrupt, -1, true},
		{os.Interrupt, 130, true},
		{syscall.SIGTERM, 143, true},
		// A status that PHP chose, e.g. 0 when the user quits the bot.
		{os.Interrupt, 0, false},
		{syscall.SIGTERM, 1, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, endedBySignal(c.sig, c.exitCode), "%v %d", c.sig, c.exitCode)
	}
}
