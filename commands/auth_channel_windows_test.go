package commands

import (
	"net"
	"testing"

	"github.com/Microsoft/go-winio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

const authChannelSupported = true

// dialAuthChannel connects to the named pipe, as the legacy CLI does.
func dialAuthChannel(getenv func(string) string) (net.Conn, error) {
	return winio.DialPipe(`\\.\pipe\`+getenv("TEST_WRAPPER_AUTH_PIPE"), nil)
}

// TestWindowsAuthChannel_AcceptError checks that the channel keeps listening after an error from Accept.
func TestWindowsAuthChannel_AcceptError(t *testing.T) {
	cmd := &exec.Cmd{}
	ch, err := openAuthChannel(testConfig(), cmd)
	require.NoError(t, err)
	defer ch.Close()
	c, ok := ch.(*pipeChannel)
	require.True(t, ok)
	c.listener = &failOnceListener{Listener: c.listener}
	var name string
	for _, e := range cmd.Env {
		if v, ok := strings.CutPrefix(e, "TEST_WRAPPER_AUTH_PIPE="); ok {
			name = v
		}
	}

	// A pipe instance only exists while Accept waits.
	dialed := make(chan error, 1)
	go func() {
		client, err := winio.DialPipe(`\\.\pipe\`+name, nil)
		if err == nil {
			t.Cleanup(func() { _ = client.Close() })
		}
		dialed <- err
	}()
	pid := make(chan int, 1)
	pid <- os.Getpid()
	conn, err := c.accept(pid)
	require.NoError(t, err)
	assert.NotNil(t, conn)
	assert.NoError(t, <-dialed)
}

type failOnceListener struct {
	net.Listener
	failed bool
}

func (l *failOnceListener) Accept() (net.Conn, error) {
	if !l.failed {
		l.failed = true
		return nil, errors.New("transient")
	}
	return l.Listener.Accept()
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
