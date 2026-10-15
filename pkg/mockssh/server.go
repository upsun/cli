package mockssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

type Server struct {
	// t and hostKey are set in NewServer.
	t       *testing.T
	hostKey ssh.Signer

	// If CertChecker is nil then a default checker will be used, which checks that the
	// certificate's public key is in CertAuthorityKeys.
	CertAuthorityKeys []ssh.PublicKey
	CertChecker       ssh.CertChecker

	// The following fields must be set before clients connect, as connection handlers read them without locking.

	// An optional CommandHandler, which responds to commands sent over SSH.
	// NewServer will give this a default using ExecHandler, which can also
	// be reused from custom handlers.
	CommandHandler CommandHandler

	// SFTPDir enables the SFTP subsystem, with this working directory.
	SFTPDir string

	// DialTCP enables local port forwarding ("direct-tcpip" channels, as with "ssh -L"),
	// by connecting to the requested host and port. Otherwise, forwarding is rejected.
	DialTCP func(host string, port int) (net.Conn, error)

	// ListenUnix enables remote forwarding of Unix sockets (as with "ssh -R /path:host:port"),
	// by listening at the requested path. Otherwise, forwarding is rejected.
	ListenUnix func(path string) (net.Listener, error)

	// listener and port are set after Start.
	listener net.Listener
	port     int

	// conns are the open connections, which Stop closes.
	connsMu sync.Mutex
	conns   map[net.Conn]struct{}
	wg      sync.WaitGroup
}

type CommandIO struct {
	StdIn  io.Reader
	StdOut io.Writer
	StdErr io.Writer
}

type CommandHandler func(conn ssh.ConnMetadata, command string, commandIO CommandIO) int

// NewServer creates and starts a local SSH server for a test.
// It must be stopped with the Server.Stop method.
// The authorityEndpoint returns SSH public keys in JSON under the key "authorities".
func NewServer(t *testing.T, authorityEndpoint string) (*Server, error) {
	hk, err := generateHostKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate host key: %v", err)
	}

	// TODO use test context
	keys, err := fetchAuthorityKeys(context.Background(), authorityEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch authority keys: %v", err)
	}

	s := &Server{t: t, hostKey: hk, conns: make(map[net.Conn]struct{})}
	s.CommandHandler = ExecHandler("", nil)
	s.CertChecker = s.defaultCertChecker()
	s.CertAuthorityKeys = keys

	if err := s.start(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Server) Port() int {
	return s.port
}

func (s *Server) HostKeyConfig() string {
	return fmt.Sprintf("[127.0.0.1]:%d %s %s",
		s.port,
		s.hostKey.PublicKey().Type(),
		base64.StdEncoding.EncodeToString(s.hostKey.PublicKey().Marshal()),
	)
}

func (s *Server) HostKey() ssh.PublicKey {
	return s.hostKey.PublicKey()
}

func (s *Server) start() error {
	t := s.t

	config := s.serverConfig()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.listener = listener
	if addr, ok := listener.Addr().(*net.TCPAddr); ok {
		s.port = addr.Port
	}
	t.Logf("Test SSH server listening at %s", listener.Addr())

	s.wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					t.Errorf("Failed to accept connection: %v", err)
				}
				return
			}
			if !s.trackConn(conn) {
				_ = conn.Close()
				return
			}
			s.wg.Go(func() {
				defer s.untrackConn(conn)
				s.handleConn(conn, config)
			})
		}
	})

	return nil
}

// Stop stops the server, closes open connections, and waits for their handlers.
func (s *Server) Stop() error {
	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	s.connsMu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.conns = nil
	s.connsMu.Unlock()
	s.wg.Wait()
	return err
}

// trackConn records an open connection, returning false if the server has stopped.
func (s *Server) trackConn(c net.Conn) bool {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.conns == nil {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrackConn(c net.Conn) {
	_ = c.Close()
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	delete(s.conns, c)
}

// ExecHandler returns a CommandHandler to execute a command in the given environment.
func ExecHandler(workingDir string, env []string) CommandHandler {
	return func(_ ssh.ConnMetadata, command string, commandIO CommandIO) int {
		c := exec.Command("bash", "-c", command)
		c.Stdout = commandIO.StdOut
		c.Stderr = commandIO.StdErr
		c.Dir = workingDir
		c.Env = env
		// Use a pipe so that Wait does not wait for the client to close stdin (e.g. with rsync).
		stdin, err := c.StdinPipe()
		if err != nil {
			_, _ = fmt.Fprintf(commandIO.StdErr, "Failed to open stdin: %v", err)
			return 1
		}
		go func() {
			_, _ = io.Copy(stdin, commandIO.StdIn)
			_ = stdin.Close()
		}()
		if err := c.Run(); err != nil {
			exitErr := &exec.ExitError{}
			if errors.As(err, &exitErr) {
				return exitErr.ExitCode()
			}
			_, _ = fmt.Fprintf(commandIO.StdErr, "Failed to execute command: %v", err)
			return 1
		}
		return 0
	}
}

func (s *Server) defaultCertChecker() ssh.CertChecker {
	return ssh.CertChecker{IsUserAuthority: func(auth ssh.PublicKey) bool {
		m := auth.Marshal()
		for _, ak := range s.CertAuthorityKeys {
			if bytes.Equal(ak.Marshal(), m) {
				return true
			}
		}
		return false
	}}
}

func (s *Server) serverConfig() *ssh.ServerConfig {
	t := s.t
	conf := &ssh.ServerConfig{}
	conf.AddHostKey(s.hostKey)
	conf.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if cert, ok := key.(*ssh.Certificate); ok {
			t.Logf("SSH certificate received from %s with key ID %s", conn.RemoteAddr(), cert.KeyId)
			return s.CertChecker.Authenticate(conn, cert)
		}
		return nil, fmt.Errorf("not accepting public key type: %s", key.Type())
	}
	conf.AuthLogCallback = func(conn ssh.ConnMetadata, method string, err error) {
		if err != nil {
			t.Logf("SSH auth log: client %s (%s), server %s, user %s, method %s, error: %s",
				conn.RemoteAddr(), conn.ClientVersion(), conn.LocalAddr(), conn.User(), method, err)
			return
		}

		t.Logf("SSH auth log: client %s, user %s, method %s", conn.RemoteAddr(), conn.User(), method)
	}

	return conf
}

// generateHostKey returns a host key, generated once, as servers may run in parallel tests.
var generateHostKey = sync.OnceValues(func() (ssh.Signer, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(privateKey)
})

func fetchAuthorityKeys(ctx context.Context, url string) ([]ssh.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch authority endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad authority endpoint status: %s", resp.Status)
	}
	var data struct {
		Authorities []string `json:"authorities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode authority response: %v", err)
	}
	sshPubKeys := make([]ssh.PublicKey, len(data.Authorities))
	for i, a := range data.Authorities {
		pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(a))
		if err != nil {
			return nil, fmt.Errorf("failed to parse authority key: %v", err)
		}
		sshPubKeys[i] = pk
	}
	return sshPubKeys, nil
}
