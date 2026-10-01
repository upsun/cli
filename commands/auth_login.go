package commands

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/auth/store"
	"github.com/upsun/cli/internal/config"
)

// The auth server only allows redirects to these local ports.
const (
	loginPortStart = 5000
	loginPortEnd   = 5010
	loginTimeout   = 30 * time.Minute
)

type browserLoginOptions struct {
	force   bool
	methods []string
	maxAge  *int
	browser string
	pipe    bool
}

func newBrowserLoginCommand(cnf *config.Config) *cobra.Command {
	exe := cnf.Application.Executable
	cmd := &cobra.Command{
		Use:     "auth:browser-login",
		Aliases: []string{"login"},
		Short:   "Log in via a browser",
		Args:    cobra.NoArgs,
		Long: fmt.Sprintf("Use this command to log in to the %s using a web browser.\n\n"+
			"It launches a temporary local website which redirects you to log in if necessary, "+
			"and then captures the resulting authorization code.\n\n"+
			"Your system's default browser will be used. You can override this using the --browser option.\n\n"+
			"Alternatively, to log in using an API token (without a browser), run: %s auth:api-token-login\n\n%s",
			cnf.Application.Name, exe, nonInteractiveAuthHelp(cnf)),
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := &browserLoginOptions{}
			opts.force, _ = cmd.Flags().GetBool("force")
			methods, _ := cmd.Flags().GetStringSlice("method")
			opts.methods = methods
			if cmd.Flags().Changed("max-age") {
				v, _ := cmd.Flags().GetInt("max-age")
				opts.maxAge = &v
			}
			opts.browser, _ = cmd.Flags().GetString("browser")
			opts.pipe, _ = cmd.Flags().GetBool("pipe")
			m, err := newAuthManager(cnf, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return runBrowserLogin(cmd, cnf, m, opts)
		},
	}
	cmd.Flags().BoolP("force", "f", false, "Log in again, even if already logged in")
	cmd.Flags().StringSlice("method", nil, "Require specific authentication method(s)")
	cmd.Flags().Int("max-age", 0, "The maximum age (in seconds) of the web authentication session")
	cmd.Flags().String("browser", "", "The browser to use to open the URL. Set 0 for none.")
	cmd.Flags().Bool("pipe", false, "Output the URL to stdout.")
	return cmd
}

func runBrowserLogin(cmd *cobra.Command, cnf *config.Config, m *auth.Manager, opts *browserLoginOptions) error {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()
	if has, err := m.HasConfiguredToken(); err != nil {
		return err
	} else if has {
		fmt.Fprintln(stderr, "Cannot log in via the browser, because an API token is set via config.")
		return &exitError{code: 1}
	}
	if !isInteractive(cmd) {
		fmt.Fprintln(stderr, "Non-interactive use of this command is not supported.")
		fmt.Fprintln(stderr, "\n"+nonInteractiveAuthHelp(cnf))
		return &exitError{code: 1}
	}
	if advice := sessionAdvice(ctx, cnf, m, "Change this using"); advice != nil {
		fmt.Fprintln(stderr, strings.Join(advice, "\n"))
		fmt.Fprintln(stderr)
	}

	if !opts.force && len(opts.methods) == 0 && opts.maxAge == nil {
		status, err := m.Status(ctx)
		if err != nil {
			return err
		}
		if status.LoggedIn {
			// Check whether the login is still valid. If so, only log in again if the user confirms.
			account, err := getMyAccount(ctx, cnf, m)
			if err == nil {
				fmt.Fprintf(stderr, "You are already logged in as %s (%s)\n",
					color.GreenString(account.Username), color.GreenString(account.Email))
				ok, err := confirm(cmd, "Log in anyway?", false)
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{code: 1}
				}
				opts.force = true
			} else {
				debugLogf("Already logged in, but a test request failed. Continuing with login: %s", err)
			}
		}
	}

	listener, err := listenOnLoginPort()
	if err != nil {
		fmt.Fprintf(stderr, "Failed to find an available port between %s and %s.\n",
			color.RedString("%d", loginPortStart), color.RedString("%d", loginPortEnd))
		fmt.Fprintln(stderr, "Check if you have unnecessary services running on these ports.")
		fmt.Fprintf(stderr, "For more options, run: %s\n", color.GreenString(cnf.Application.Executable+" help login"))
		return &exitError{code: 1}
	}
	localURL := "http://" + listener.Addr().String()

	verifier := randomString()
	prompt := "consent"
	if opts.force {
		prompt = "consent select_account"
	}
	ls := &loginServer{
		cnf:       cnf,
		localURL:  localURL,
		authorize: m.Settings.AuthorizeURL,
		clientID:  m.Settings.ClientID,
		state:     randomString(),
		challenge: pkceChallenge(verifier),
		prompt:    prompt,
		methods:   opts.methods,
		maxAge:    opts.maxAge,
		result:    make(chan loginResult, 1),
	}
	srv := &http.Server{Handler: ls, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	switch {
	case opts.pipe:
		fmt.Fprintln(cmd.OutOrStdout(), localURL)
	case opts.browser != "0" && openURL(localURL, opts.browser):
		fmt.Fprintf(stderr, "Opened URL: %s\n", color.GreenString(localURL))
		fmt.Fprintln(stderr, "Please use the browser to log in.")
	default:
		fmt.Fprintln(stderr, "Please open the following URL in a browser and log in:")
		fmt.Fprintln(stderr, color.GreenString(localURL))
	}
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, color.New(color.Bold).Sprint("Help:"))
	fmt.Fprintln(stderr, "  Leave this command running during login.")
	fmt.Fprintln(stderr, "  If you need to quit, use Ctrl+C.")
	fmt.Fprintln(stderr)

	var res loginResult
	select {
	case res = <-ls.result:
	case <-time.After(loginTimeout):
		fmt.Fprintln(stderr, "Login timed out after 30 minutes")
		fmt.Fprintln(stderr)
	case <-ctx.Done():
		return ctx.Err()
	}
	// Allow a little time for the final page to be displayed in the browser.
	time.Sleep(100 * time.Millisecond)

	if res.code == "" {
		fmt.Fprintln(stderr, "Failed to get an authorization code.")
		fmt.Fprintln(stderr)
		switch {
		case res.err != "" && res.errDescription != "":
			fmt.Fprintln(stderr, "  OAuth 2.0 error: "+color.RedString(res.err))
			fmt.Fprintln(stderr, "  Description: "+res.errDescription)
			if res.errHint != "" {
				fmt.Fprintln(stderr, "  Hint: "+res.errHint)
			}
			fmt.Fprintln(stderr)
		case res.errDescription != "":
			fmt.Fprintln(stderr, res.errDescription)
			fmt.Fprintln(stderr)
		}
		fmt.Fprintln(stderr, "Please try again.")
		return &exitError{code: 1}
	}

	fmt.Fprintln(stderr, "Login information received. Verifying...")
	entry, err := m.OAuth.ExchangeCode(ctx, res.code, verifier, localURL)
	if err != nil {
		return fmt.Errorf("failed to exchange the authorization code: %w", err)
	}
	if err := saveLogin(cmd, cnf, m, entry, ""); err != nil {
		return err
	}

	if entry.RefreshToken == "" {
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, color.New(color.Bold, color.FgYellow).Sprint("Warning:"))
		fmt.Fprintln(stderr, "No refresh token is available. This will cause frequent login errors.")
		fmt.Fprintln(stderr, "Please contact support.")
		fmt.Fprintf(stderr, "For internal use: the OAuth 2 client is probably misconfigured (client ID: %s).\n",
			color.YellowString(m.Settings.ClientID))
	}
	return nil
}

// saveLogin logs out of the previous session, saves the new tokens, and runs the legacy CLI's post-login steps.
// For an API token login, entry holds the tokens from exchanging apiToken.
func saveLogin(cmd *cobra.Command, cnf *config.Config, m *auth.Manager, entry *store.Entry, apiToken string) error {
	ctx := cmd.Context()
	id := m.Settings.SessionID
	if err := m.Logout(ctx, id); err != nil {
		return err
	}
	if apiToken != "" {
		if err := m.Save(ctx, auth.APITokenSessionID(apiToken), entry); err != nil {
			return err
		}
		entry = &store.Entry{APIToken: apiToken}
	}
	if err := m.Save(ctx, id, entry); err != nil {
		return err
	}
	return runLegacyAuthHook(cmd, cnf, "auth:post-login")
}

type myAccount struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

// getMyAccount fetches the current user, without offering a login.
func getMyAccount(ctx context.Context, cnf *config.Config, m *auth.Manager) (*myAccount, error) {
	u, err := url.JoinPath(m.Settings.BaseURL, "users", "me")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := auth.NewClient(m, auth.NewHTTPClient(cnf, m.Settings).Transport).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	var a myAccount
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

func listenOnLoginPort() (net.Listener, error) {
	var lastErr error
	for port := loginPortStart; port <= loginPortEnd; port++ {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			return l, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func randomString() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// pkceChallenge applies the PKCE S256 transformation (RFC 7636).
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type loginResult struct {
	code                         string
	err, errDescription, errHint string
}

// loginServer is the local web server that starts the OAuth 2.0 flow and receives the authorization code.
type loginServer struct {
	cnf       *config.Config
	localURL  string
	authorize string
	clientID  string
	state     string
	challenge string
	prompt    string
	methods   []string
	maxAge    *int
	result    chan loginResult
}

type loginPage struct {
	status   int
	location string
	title    string
	content  string
}

func (s *loginServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	p := s.handle(r.URL.Query())
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if p.location != "" {
		w.Header().Set("Location", p.location)
	}
	w.WriteHeader(p.status)
	_, _ = w.Write([]byte(s.render(p)))
}

func (s *loginServer) handle(q url.Values) *loginPage {
	switch {
	case q.Has("state") && q.Has("code"):
		// The response after a successful OAuth 2.0 redirect.
		if q.Get("state") != s.state {
			return s.reportError("Invalid state parameter", "", "")
		}
		if q.Has("code_challenge") && q.Get("code_challenge") != s.challenge {
			return s.reportError("Invalid returned code_challenge parameter", "", "")
		}
		s.send(loginResult{code: q.Get("code")})
		return &loginPage{
			status:   http.StatusFound,
			location: s.localURL + "/?done",
			content:  "<p>Authentication response received, please wait...</p>",
		}
	case q.Has("done"):
		return &loginPage{
			status:  http.StatusOK,
			title:   "Successfully logged in",
			content: "<p>You can return to the command line</p>",
		}
	case q.Has("error"):
		return s.reportError(q.Get("error_description"), q.Get("error"), q.Get("error_hint"))
	}
	authURL := s.authorizeURL()
	return &loginPage{
		status:   http.StatusFound,
		location: authURL,
		content:  `<p><a href="` + html.EscapeString(authURL) + `">Log in</a>.</p>`,
	}
}

// send reports a result to the command, once.
func (s *loginServer) send(r loginResult) {
	select {
	case s.result <- r:
	default:
	}
}

func (s *loginServer) reportError(message, oauthErr, hint string) *loginPage {
	p := &loginPage{status: http.StatusUnauthorized, title: "Error"}
	if oauthErr != "" {
		p.content += `<p class="error"><code>` + html.EscapeString(oauthErr) + `</code></p>`
	}
	if message != "" {
		p.content += `<p class="error">` + html.EscapeString(message) + `</p>`
	}
	if hint != "" {
		p.content += `<p class="error error-hint">` + html.EscapeString(hint) + `</p>`
	}
	if message != "" || oauthErr != "" || hint != "" {
		s.send(loginResult{err: oauthErr, errDescription: message, errHint: hint})
	}
	p.content += "<p>Please try again</p>"
	return p
}

func (s *loginServer) authorizeURL() string {
	params := url.Values{
		"redirect_uri":          {s.localURL},
		"state":                 {s.state},
		"client_id":             {s.clientID},
		"prompt":                {s.prompt},
		"response_type":         {"code"},
		"code_challenge":        {s.challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {"offline_access"},
	}
	if len(s.methods) > 0 {
		params.Set("amr", strings.Join(s.methods, " "))
	}
	if s.maxAge != nil {
		params.Set("max_age", strconv.Itoa(*s.maxAge))
	}
	sep := "?"
	if strings.Contains(s.authorize, "?") {
		sep = "&"
	}
	// PHP's http_build_query with RFC 3986 encoding uses %20 for spaces.
	return s.authorize + sep + strings.ReplaceAll(params.Encode(), "+", "%20")
}

var loginPlaceholder = regexp.MustCompile(`\{\{\s*(content|title)\s*}}`)

func (s *loginServer) render(p *loginPage) string {
	body := "<h1>" + p.title + "</h1>" + p.content
	if tpl := s.cnf.BrowserLogin.Body; tpl != "" {
		body = loginPlaceholder.ReplaceAllStringFunc(tpl, func(m string) string {
			if strings.Contains(m, "content") {
				return p.content
			}
			return p.title
		})
	}
	var css string
	if s.cnf.BrowserLogin.CSS != "" {
		css = "<style>\n" + s.cnf.BrowserLogin.CSS + "\n</style>\n"
	}
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <title>` + html.EscapeString(s.cnf.Application.Name) + `: Authentication (temporary URL)</title>
    <style>
        html {
            font-family: "Helvetica Neue", Helvetica, Arial, sans-serif;
            font-weight: 300;
            background-color: #eee;
        }
        body {
            text-align: center;
        }
        img.icon {
            display: block;
            margin: 3em auto 1em;
        }
        h1 {
            font-weight: 100;
            margin: 1em auto;
        }
        .error {
            color: darkred;
        }
        .error-hint {
            font-style: oblique;
        }
    </style>
    ` + css + `</head>
<body>
` + body + `
</body>
</html>
`
}
