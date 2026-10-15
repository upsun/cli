package mockssh

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func (s *Server) handleConn(conn net.Conn, config *ssh.ServerConfig) {
	t := s.t
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		t.Logf("Handshake failed: %v", err)
		return
	}
	defer sshConn.Close()

	t.Logf("Handling SSH connection from %s", sshConn.RemoteAddr())

	// Listeners for remote forwarding, closed with the connection.
	var (
		listenersMu sync.Mutex
		listeners   []net.Listener
	)
	var wg sync.WaitGroup
	defer func() {
		listenersMu.Lock()
		for _, l := range listeners {
			_ = l.Close()
		}
		listenersMu.Unlock()
		wg.Wait()
	}()

	wg.Go(func() {
		for req := range reqs {
			if req.Type == "streamlocal-forward@openssh.com" {
				if l := s.forwardUnix(sshConn, req, &wg); l != nil {
					listenersMu.Lock()
					listeners = append(listeners, l)
					listenersMu.Unlock()
				}
				continue
			}
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	})

	for newChannel := range chans {
		switch newChannel.ChannelType() {
		case "session":
			wg.Go(func() { s.handleSession(sshConn, newChannel) })
		case "direct-tcpip":
			wg.Go(func() { s.handleDirectTCPIP(newChannel) })
		default:
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
		}
	}
}

// handleSession handles a session channel, which runs a command or the SFTP subsystem.
func (s *Server) handleSession(conn ssh.ConnMetadata, newChannel ssh.NewChannel) {
	t := s.t
	channel, requests, err := newChannel.Accept()
	if err != nil {
		t.Errorf("Failed to accept channel: %v", err)
		return
	}
	defer channel.Close()

	exitWithStatus := make(chan int, 1)
	go func() {
		for req := range requests {
			var payload struct{ Value string }
			switch {
			case req.Type == "exec" && ssh.Unmarshal(req.Payload, &payload) == nil:
				_ = req.Reply(true, nil)
				t.Logf("Handling command: %s", payload.Value)
				exitWithStatus <- s.CommandHandler(conn, payload.Value, CommandIO{
					StdIn:  channel,
					StdOut: channel,
					StdErr: channel.Stderr(),
				})
				return
			case req.Type == "subsystem" && s.SFTPDir != "" &&
				ssh.Unmarshal(req.Payload, &payload) == nil && payload.Value == "sftp":
				_ = req.Reply(true, nil)
				t.Logf("Handling SFTP session in %s", s.SFTPDir)
				exitWithStatus <- serveSFTP(channel, s.SFTPDir)
				return
			default:
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
			}
		}
	}()

	select {
	case status := <-exitWithStatus:
		_, err = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
		if err != nil {
			t.Errorf("Failed to send exit status: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("Timed out")
	}
}

func serveSFTP(channel ssh.Channel, dir string) int {
	server, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(dir))
	if err != nil {
		return 1
	}
	if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
		return 1
	}
	return 0
}

// handleDirectTCPIP handles local port forwarding, if DialTCP is set.
func (s *Server) handleDirectTCPIP(newChannel ssh.NewChannel) {
	t := s.t
	if s.DialTCP == nil {
		_ = newChannel.Reject(ssh.Prohibited, "port forwarding is disabled")
		return
	}
	// See: https://datatracker.ietf.org/doc/html/rfc4254#section-7.2
	var payload struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, "invalid payload")
		return
	}
	t.Logf("Forwarding a connection to %s:%d", payload.Host, payload.Port)
	target, err := s.DialTCP(payload.Host, int(payload.Port))
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	channel, requests, err := newChannel.Accept()
	if err != nil {
		_ = target.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	proxy(channel, target)
}

// forwardUnix handles a request for remote forwarding of a Unix socket, if ListenUnix is set.
// It returns the listener, or nil if the request is rejected.
func (s *Server) forwardUnix(conn *ssh.ServerConn, req *ssh.Request, wg *sync.WaitGroup) net.Listener {
	t := s.t
	var payload struct{ Path string }
	if s.ListenUnix == nil || ssh.Unmarshal(req.Payload, &payload) != nil {
		_ = req.Reply(false, nil)
		return nil
	}
	l, err := s.ListenUnix(payload.Path)
	if err != nil {
		t.Logf("Failed to listen at %s: %v", payload.Path, err)
		_ = req.Reply(false, nil)
		return nil
	}
	_ = req.Reply(true, nil)
	t.Logf("Forwarding connections from the socket %s", payload.Path)
	wg.Go(func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			wg.Go(func() {
				// See: https://github.com/openssh/openssh-portable/blob/master/PROTOCOL
				channel, requests, err := conn.OpenChannel("forwarded-streamlocal@openssh.com",
					ssh.Marshal(struct{ Path, Reserved string }{Path: payload.Path}))
				if err != nil {
					_ = c.Close()
					return
				}
				go ssh.DiscardRequests(requests)
				proxy(channel, c)
			})
		}
	})
	return l
}

// proxy copies data in both directions until either side is closed.
func proxy(channel ssh.Channel, conn net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(channel, conn)
		_ = channel.CloseWrite()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, channel)
		if c, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	_ = channel.Close()
	_ = conn.Close()
}
