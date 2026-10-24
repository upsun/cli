package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
)

// authChannelRequest is a request from the legacy CLI, for the same results as the auth:internal command.
type authChannelRequest struct {
	Command   string `json:"command"` // "token" or "status"
	SessionID string `json:"session_id"`
	Rejected  string `json:"rejected,omitempty"`
}

type authChannelResponse struct {
	Result        any                      `json:"result,omitempty"`
	LoginRequired *auth.LoginRequiredError `json:"login_required,omitempty"`
	Error         string                   `json:"error,omitempty"`
}

// authChannelTimeout bounds each request, as a refresh can wait for another process's lock.
const authChannelTimeout = 5 * time.Minute

// prepareAuthChannel gives a legacy CLI command one end of a socket pair, as an inherited file descriptor, and serves
// auth requests on the other end while it runs. This replaces running the auth:internal command for each request.
//
// The descriptor number and this process's ID are passed in environment variables: the legacy CLI only uses the
// descriptor in the process that this one started, not in processes it forks. Without them, or if a request fails, it
// runs auth:internal instead.
func prepareAuthChannel(cnf *config.Config, stderr io.Writer) func(cmd *exec.Cmd) (func(), error) {
	return func(cmd *exec.Cmd) (func(), error) {
		conn, child, err := newSocketPair()
		if err != nil {
			debugLogf("Auth channel not available: %s", err)
			return func() {}, nil
		}
		fd := 3 + len(cmd.ExtraFiles)
		cmd.ExtraFiles = append(cmd.ExtraFiles, child)
		prefix := cnf.Application.EnvPrefix
		cmd.Env = append(cmd.Env,
			prefix+"WRAPPER_AUTH_FD="+strconv.Itoa(fd),
			prefix+"WRAPPER_PID="+strconv.Itoa(os.Getpid()),
		)
		s := &authChannelServer{cnf: cnf, stderr: stderr, managers: map[string]*auth.Manager{}}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.serve(ctx, conn)
		}()
		return func() {
			cancel()
			_ = conn.Close()
			_ = child.Close()
			<-done
		}, nil
	}
}

type authChannelServer struct {
	cnf    *config.Config
	stderr io.Writer

	mu       sync.Mutex
	managers map[string]*auth.Manager // By session ID.
}

// serve answers requests in order, each as a line of JSON, until the connection is closed.
func (s *authChannelServer) serve(ctx context.Context, conn net.Conn) {
	r := bufio.NewReader(conn)
	enc := json.NewEncoder(conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		if err := enc.Encode(s.handle(ctx, line)); err != nil {
			return
		}
	}
}

func (s *authChannelServer) handle(ctx context.Context, line []byte) *authChannelResponse {
	var req authChannelRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return &authChannelResponse{Error: fmt.Sprintf("invalid request: %s", err)}
	}
	debugLogf("Auth channel request: %s", req.Command)
	ctx, cancel := context.WithTimeout(ctx, authChannelTimeout)
	defer cancel()
	result, err := s.result(ctx, &req)
	if lerr, ok := auth.AsLoginRequired(err); ok {
		return &authChannelResponse{LoginRequired: lerr}
	}
	if err != nil {
		return &authChannelResponse{Error: err.Error()}
	}
	return &authChannelResponse{Result: result}
}

func (s *authChannelServer) result(ctx context.Context, req *authChannelRequest) (any, error) {
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
func (s *authChannelServer) manager(sessionID string) (*auth.Manager, error) {
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
