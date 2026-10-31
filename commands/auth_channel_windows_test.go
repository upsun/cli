package commands

import (
	"net"
	"testing"

	"github.com/Microsoft/go-winio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// dialAuthChannel connects to the named pipe, as the legacy CLI does.
func dialAuthChannel(getenv func(string) string) (net.Conn, error) {
	return winio.DialPipe(`\\.\pipe\`+getenv("TEST_WRAPPER_AUTH_PIPE"), nil)
}

func TestWindowsPipeSDDL(t *testing.T) {
	t.Logf("This process is elevated: %v", windows.GetCurrentProcessToken().IsElevated())
	sid, err := windows.StringToSid("S-1-5-21-1-2-3-1001")
	require.NoError(t, err)
	cases := []struct {
		elevated bool
		want     string
	}{
		{false, "O:S-1-5-21-1-2-3-1001D:P(A;;GA;;;S-1-5-21-1-2-3-1001)"},
		{true, "O:S-1-5-21-1-2-3-1001D:P(A;;GA;;;S-1-5-21-1-2-3-1001)S:(ML;;NW;;;HI)"},
	}
	for _, c := range cases {
		sddl := pipeSDDL(sid, c.elevated)
		assert.Equal(t, c.want, sddl)
		_, err = windows.SecurityDescriptorFromString(sddl)
		assert.NoError(t, err)
	}
}
