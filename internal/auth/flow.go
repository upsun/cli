// internal/auth/flow.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/session"
)

// BrowserFlowOptions configures the browser login flow.
type BrowserFlowOptions struct {
	Force   bool
	Methods []string
	MaxAge  *int
	// Stderr is the writer used for user-facing messages (local URL, instructions).
	// Defaults to os.Stderr if nil.
	Stderr io.Writer
}

// BrowserFlow orchestrates the OAuth2 PKCE browser login flow.
type BrowserFlow struct {
	cfg     *config.Config
	OpenURL func(string) error // override for testing; defaults to opening the system browser
}

// NewBrowserFlow creates a BrowserFlow with system browser support.
func NewBrowserFlow(cfg *config.Config) *BrowserFlow {
	return &BrowserFlow{cfg: cfg, OpenURL: openSystemBrowser}
}

// Run performs the full PKCE flow and returns a session on success.
func (f *BrowserFlow) Run(ctx context.Context, opts BrowserFlowOptions) (*session.Session, error) {
	verifier, err := GenerateVerifier()
	if err != nil {
		return nil, err
	}
	challenge := VerifierToChallenge(verifier)
	state, err := GenerateVerifier() // state is any random string
	if err != nil {
		return nil, err
	}

	// Find an available port in 5000–5010.
	listener, port, err := findPort(5000, 5010)
	if err != nil {
		return nil, fmt.Errorf("find available port (5000-5010): %w", err)
	}
	localURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Resolve the writer for user-facing messages.
	w := opts.Stderr
	if w == nil {
		w = os.Stderr
	}

	// Build the authorization URL ahead of time.
	authURL := f.buildAuthURL(localURL, challenge, state, opts)

	// Channel to receive the auth code from the callback handler.
	codeCh := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(hw http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// If no code param, redirect to the authorization server.
		if q.Get("code") == "" && q.Get("error") == "" {
			http.Redirect(hw, r, authURL, http.StatusFound)
			return
		}
		if errParam := q.Get("error"); errParam != "" {
			select {
			case codeCh <- callbackResult{err: fmt.Errorf("OAuth error: %s — %s", errParam, q.Get("error_description"))}:
			default:
			}
			fmt.Fprintln(hw, "Login failed. You may close this tab.")
			return
		}
		if q.Get("state") != state {
			select {
			case codeCh <- callbackResult{err: fmt.Errorf("state mismatch")}:
			default:
			}
			fmt.Fprintln(hw, "Login failed (invalid state). You may close this tab.")
			return
		}
		select {
		case codeCh <- callbackResult{code: q.Get("code"), redirectURI: localURL}:
		default:
		}
		fmt.Fprintln(hw, "Login successful. You may close this tab.")
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	// Open browser or print URL.
	if err := f.OpenURL(localURL); err != nil {
		fmt.Fprintf(w, "Please open the following URL in a browser:\n%s\n", localURL)
	} else {
		fmt.Fprintf(w, "Opened URL: %s\nPlease use the browser to log in.\n", localURL)
	}

	// Wait for callback (30-minute timeout).
	select {
	case result := <-codeCh:
		if result.err != nil {
			return nil, result.err
		}
		return f.exchangeCode(ctx, result.code, verifier, result.redirectURI)
	case <-time.After(30 * time.Minute):
		return nil, fmt.Errorf("login timed out after 30 minutes")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type callbackResult struct {
	code        string
	redirectURI string
	err         error
}

// oauth2AuthorizeURL resolves the OAuth2 authorize endpoint URL.
// Priority: {EnvPrefix}AUTH_URL env → cfg.API.AuthURL → cfg.API.OAuth2AuthorizeURL
func (f *BrowserFlow) oauth2AuthorizeURL() string {
	authURL := os.Getenv(f.cfg.Application.EnvPrefix + "AUTH_URL")
	if authURL == "" {
		authURL = f.cfg.API.AuthURL
	}
	if authURL != "" {
		return strings.TrimRight(authURL, "/") + "/oauth2/authorize"
	}
	return f.cfg.API.OAuth2AuthorizeURL
}

// oauth2TokenURLFlow resolves the OAuth2 token endpoint URL for this flow.
func (f *BrowserFlow) oauth2TokenURLFlow() string {
	authURL := os.Getenv(f.cfg.Application.EnvPrefix + "AUTH_URL")
	if authURL == "" {
		authURL = f.cfg.API.AuthURL
	}
	if authURL != "" {
		return strings.TrimRight(authURL, "/") + "/oauth2/token"
	}
	return f.cfg.API.OAuth2TokenURL
}

func (f *BrowserFlow) buildAuthURL(localURL, challenge, state string, opts BrowserFlowOptions) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {f.cfg.API.OAuth2ClientID},
		"redirect_uri":          {localURL},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"scope":                 {"offline_access"},
	}
	prompt := "consent"
	if opts.Force {
		prompt = "consent select_account"
	}
	params.Set("prompt", prompt)
	if len(opts.Methods) > 0 {
		params.Set("acr_values", strings.Join(opts.Methods, " "))
	}
	if opts.MaxAge != nil {
		params.Set("max_age", fmt.Sprintf("%d", *opts.MaxAge))
	}
	return f.oauth2AuthorizeURL() + "?" + params.Encode()
}

func (f *BrowserFlow) exchangeCode(ctx context.Context, code, verifier, redirectURI string) (*session.Session, error) {
	tokenURL := f.oauth2TokenURLFlow()
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if f.cfg.API.OAuth2ClientID != "" {
		req.SetBasicAuth(f.cfg.API.OAuth2ClientID, "")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("code exchange: server returned %d", resp.StatusCode)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("code exchange: decode response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("code exchange: %s", result.Error)
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Unix()
	return &session.Session{
		AccessToken:  result.AccessToken,
		TokenType:    result.TokenType,
		Expires:      expiry,
		RefreshToken: result.RefreshToken,
	}, nil
}

func findPort(start, end int) (net.Listener, int, error) {
	for port := start; port <= end; port++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return l, port, nil
		}
	}
	return nil, 0, fmt.Errorf("failed to find available port between %d and %d", start, end)
}

func openSystemBrowser(url string) error {
	return openBrowser(url)
}
