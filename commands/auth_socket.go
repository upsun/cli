package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
)

// authSocketRequest is a request from the legacy CLI, for the same results as the auth:internal command.
type authSocketRequest struct {
	Command   string `json:"command"` // "token" or "status"
	SessionID string `json:"session_id"`
	Rejected  string `json:"rejected,omitempty"`
}

type authSocketResponse struct {
	Result        any                      `json:"result,omitempty"`
	LoginRequired *auth.LoginRequiredError `json:"login_required,omitempty"`
	Error         string                   `json:"error,omitempty"`
}

// authSocketTimeout bounds each request, as a refresh can wait for another process's lock.
const authSocketTimeout = 5 * time.Minute

// startAuthSocket serves auth requests from the legacy CLI on a Unix socket in a private directory, so that it can get
// tokens from this process instead of running the auth:internal command. It returns the environment variable that
// passes the socket's path, and a function that stops the server.
func startAuthSocket(cnf *config.Config, stderr io.Writer) (env []string, stop func(), err error) {
	// Without the socket, the legacy CLI runs auth:internal instead.
	noSocket := func() {}
	if runtime.GOOS == "windows" {
		return nil, noSocket, nil
	}
	dir, l, err := listenAuthSocket(cnf.Application.Slug + "-auth-")
	if err != nil {
		debugLogf("Failed to start the auth socket: %s", err)
		return nil, noSocket, nil
	}
	path := l.Addr().String()
	s := &authSocketServer{cnf: cnf, stderr: stderr, managers: map[string]*auth.Manager{}}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.serve(ctx, l, &wg)
	}()
	stop = func() {
		cancel()
		_ = l.Close()
		wg.Wait()
		_ = os.RemoveAll(dir)
	}
	return []string{cnf.Application.EnvPrefix + "WRAPPER_AUTH_SOCKET=" + path}, stop, nil
}

// maxSocketPath is the shortest limit on a Unix socket's path, from macOS (Linux allows 107 bytes).
const maxSocketPath = 103

// listenAuthSocket listens on a socket in a new private directory, in the first base directory with a short enough
// path. It returns the directory, to remove after the listener is closed.
func listenAuthSocket(prefix string) (dir string, l net.Listener, err error) {
	var bases []string
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		bases = append(bases, d)
	}
	bases = append(bases, os.TempDir(), "/tmp")
	err = errors.New("no directory with a short enough path")
	for _, base := range bases {
		// MkdirTemp adds up to 20 characters to the prefix.
		if len(filepath.Join(base, prefix+"12345678901234567890", "auth.sock")) > maxSocketPath {
			continue
		}
		dir, err = os.MkdirTemp(base, prefix)
		if err != nil {
			continue
		}
		l, err = net.Listen("unix", filepath.Join(dir, "auth.sock"))
		if err == nil {
			return dir, l, nil
		}
		_ = os.RemoveAll(dir) //nolint:gosec // the directory was created by MkdirTemp
	}
	return "", nil, err
}

type authSocketServer struct {
	cnf    *config.Config
	stderr io.Writer

	mu       sync.Mutex
	managers map[string]*auth.Manager // By session ID.
}

func (s *authSocketServer) serve(ctx context.Context, l net.Listener, wg *sync.WaitGroup) {
	for {
		conn, err := l.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				debugLogf("Auth socket: %s", err)
			}
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()
			s.handle(ctx, conn)
		}()
	}
}

// handle answers one request per connection, each as a line of JSON.
func (s *authSocketServer) handle(ctx context.Context, conn net.Conn) {
	ctx, cancel := context.WithTimeout(ctx, authSocketTimeout)
	defer cancel()
	_ = conn.SetDeadline(time.Now().Add(authSocketTimeout))
	var req authSocketRequest
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err == nil {
		err = json.Unmarshal(line, &req)
	}
	var resp authSocketResponse
	if err == nil {
		debugLogf("Auth socket request: %s", req.Command)
	}
	if err != nil {
		resp.Error = fmt.Sprintf("invalid request: %s", err)
	} else {
		result, err := s.result(ctx, &req)
		if lerr, ok := auth.AsLoginRequired(err); ok {
			resp.LoginRequired = lerr
		} else if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = result
		}
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func (s *authSocketServer) result(ctx context.Context, req *authSocketRequest) (any, error) {
	m, err := s.manager(req.SessionID)
	if err != nil {
		return nil, err
	}
	switch req.Command {
	case "token":
		return m.Token(ctx, req.Rejected)
	case "status":
		return m.Status(ctx)
	default:
		return nil, fmt.Errorf("unknown command: %s", req.Command)
	}
}

// manager returns a manager for the session that the legacy CLI uses, which session:switch can change.
func (s *authSocketServer) manager(sessionID string) (*auth.Manager, error) {
	if err := config.ValidateSessionID(sessionID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.managers[sessionID]; ok {
		return m, nil
	}
	m, err := newAuthManager(s.cnf, s.stderr)
	if err != nil {
		return nil, err
	}
	m.Settings.SessionID = sessionID
	s.managers[sessionID] = m
	return m, nil
}
