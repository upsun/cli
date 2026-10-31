package tests

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// runningLogin is a browser login command that runs in the background.
type runningLogin struct {
	stdout, stderr syncBuffer
	exited         chan struct{}
	err            error
	localURL       string
}

// startBrowserLogin starts auth:browser-login, and waits for the URL of its local server.
func startBrowserLogin(t *testing.T, f *cmdFactory, args ...string) *runningLogin {
	t.Helper()
	cmd := f.buildCommand(append([]string{"auth:browser-login"}, args...)...)
	l := &runningLogin{exited: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = &l.stdout, &l.stderr
	cmd.WaitDelay = 5 * time.Second
	t.Log("Running:", cmd)
	require.NoError(t, cmd.Start())
	go func() {
		l.err = cmd.Wait()
		close(l.exited)
	}()
	// Otherwise the command waits for 30 minutes.
	t.Cleanup(func() {
		select {
		case <-l.exited:
		default:
			_ = cmd.Process.Kill()
			<-l.exited
		}
	})
	re := regexp.MustCompile(`http://127\.0\.0\.1:\d+`)
	if !assert.Eventually(t, func() bool {
		l.localURL = re.FindString(l.stdout.String() + l.stderr.String())
		return l.localURL != ""
	}, 20*time.Second, 50*time.Millisecond) {
		t.Fatalf("no local URL found in the output: %s", l.stderr.String())
	}
	return l
}

// wait waits for the command to exit, and returns its output and error.
func (l *runningLogin) wait(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	select {
	case <-l.exited:
	case <-time.After(30 * time.Second):
		t.Fatalf("the command did not exit; stderr:\n%s", l.stderr.String())
	}
	t.Logf("stderr:\n%s", l.stderr.String())
	return l.stdout.String(), l.stderr.String(), l.err
}

// running reports whether the command is still running.
func (l *runningLogin) running() bool {
	select {
	case <-l.exited:
		return false
	default:
		return true
	}
}

// get requests a path from the local server with a host header, without following redirects.
func (l *runningLogin) get(t *testing.T, path, host string) (status int, header http.Header, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, l.localURL+path, http.NoBody)
	require.NoError(t, err)
	if host != "" {
		req.Host = host
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, string(b)
}

// authorizeURL returns the URL that the local server redirects to, to start the login.
func (l *runningLogin) authorizeURL(t *testing.T) *url.URL {
	t.Helper()
	status, header, _ := l.get(t, "/", "")
	require.Equal(t, http.StatusFound, status)
	u, err := url.Parse(header.Get("Location"))
	require.NoError(t, err)
	return u
}

// logIn completes the login, as a browser would, and returns the final page.
func (l *runningLogin) logIn(t *testing.T) string {
	t.Helper()
	resp, err := http.Get(l.localURL) //nolint:noctx // a test request
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

// TestAuthBrowserLogin_MethodMaxAgePipe checks that the required authentication methods and max age are sent to the
// auth server, and that --pipe prints the URL to stdout.
func TestAuthBrowserLogin_MethodMaxAgePipe(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newInteractiveGoAuthFactory(t, apiServer.URL, authServer.URL)
	l := startBrowserLogin(t, f, "--pipe", "--method", "mfa,sso:acme", "--max-age", "0")

	u := l.authorizeURL(t)
	assert.Equal(t, authServer.URL+"/oauth2/authorize", u.Scheme+"://"+u.Host+u.Path)
	q := u.Query()
	assert.Equal(t, "mfa sso:acme", q.Get("amr"))
	assert.Equal(t, "0", q.Get("max_age"))
	assert.Equal(t, "consent", q.Get("prompt"))
	assert.Equal(t, l.localURL, q.Get("redirect_uri"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "offline_access", q.Get("scope"))
	// Spaces are encoded as in the legacy CLI.
	assert.Contains(t, u.RawQuery, "amr=mfa%20sso%3Aacme")

	assert.Contains(t, l.logIn(t), "<h1>Successfully logged in</h1><p>You can return to the command line</p>")
	stdout, stderr, err := l.wait(t)
	require.NoError(t, err)
	assert.Equal(t, l.localURL+"\n", stdout)
	assert.NotContains(t, stderr, "Please open the following URL")
	assert.Contains(t, stderr, "You are logged in.")
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
}

// TestAuthBrowserLogin_ErrorPage checks the local server's responses to requests that do not complete the login, in
// a CLI with a custom login page.
func TestAuthBrowserLogin_ErrorPage(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	custom := "\nbrowser_login:\n  body: '<main><h2>{{ title }}</h2>{{content}}</main>'\n  css: 'main { color: teal; }'\n"
	require.NoError(t, os.WriteFile(configFile, append(baseConfig, custom...), 0o600))

	f := newInteractiveGoAuthFactory(t, "", authServer.URL)
	f.useConfig(configFile)
	l := startBrowserLogin(t, f, "--browser=0")
	assert.Contains(t, l.stderr.String(), "Please open the following URL in a browser and log in:\n"+l.localURL)
	state := l.authorizeURL(t).Query().Get("state")
	require.NotEmpty(t, state)

	status, _, _ := l.get(t, "/favicon.ico", "")
	assert.Equal(t, http.StatusNotFound, status)

	// Other hosts are rejected, against DNS rebinding.
	status, _, body := l.get(t, "/", "localhost"+strings.TrimPrefix(l.localURL, "http://127.0.0.1"))
	assert.Equal(t, http.StatusMisdirectedRequest, status)
	assert.Equal(t, "Invalid host\n", body)

	status, _, body = l.get(t, "/?code=abc&state=wrong", "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, `<main><h2>Error</h2><p class="error">Invalid state parameter</p>`)
	assert.Contains(t, body, "<style>\nmain { color: teal; }\n</style>")
	assert.Contains(t, body, "<title>Platform Test CLI: Authentication (temporary URL)</title>")

	// An error without the login's state does not end it.
	status, _, body = l.get(t, "/?error=access_denied", "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, `<p class="error"><code>access_denied</code></p><p>Please try again</p>`)
	time.Sleep(200 * time.Millisecond)
	assert.True(t, l.running(), "the login must not end")

	q := url.Values{
		"error": {"access_denied"}, "error_description": {"<b>Denied</b>"}, "error_hint": {"Ask"}, "state": {state},
	}
	status, _, body = l.get(t, "/?"+q.Encode(), "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, `<p class="error"><code>access_denied</code></p>`+
		`<p class="error">&lt;b&gt;Denied&lt;/b&gt;</p><p class="error error-hint">Ask</p>`)

	_, stderr, err := l.wait(t)
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Failed to get an authorization code.\n\n"+
		"  OAuth 2.0 error: access_denied\n  Description: <b>Denied</b>\n  Hint: Ask\n\nPlease try again.\n")
	assert.Empty(t, authServer.AuthorizePrompts(), "the auth server must not be used")
}

// TestAuthBrowserLogin_CodeChallengeMismatch checks that a login ends if the redirect returns another PKCE challenge.
func TestAuthBrowserLogin_CodeChallengeMismatch(t *testing.T) {
	t.Parallel()
	f := newInteractiveGoAuthFactory(t, "", "")
	l := startBrowserLogin(t, f, "--pipe")
	state := l.authorizeURL(t).Query().Get("state")

	q := url.Values{"code": {"abc"}, "state": {state}, "code_challenge": {"other"}}
	status, _, body := l.get(t, "/?"+q.Encode(), "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "Invalid returned code_challenge parameter")

	_, stderr, err := l.wait(t)
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Failed to get an authorization code.\n\n"+
		"Invalid returned code_challenge parameter\n\nPlease try again.\n")
}

// TestAuthBrowserLogin_BrowserOption checks that --browser sets the command to open the URL, with its arguments,
// and that a browser that is not found is not used.
func TestAuthBrowserLogin_BrowserOption(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake browser is a shell script")
	}
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	dir := t.TempDir()
	argsFile, browser := filepath.Join(dir, "args"), filepath.Join(dir, "browser")
	script := "#!/bin/sh\necho \"$@\" > '" + argsFile + "'\nshift\nexec curl -fsSL -o /dev/null \"$1\"\n"
	require.NoError(t, os.WriteFile(browser, []byte(script), 0o755))

	f := newInteractiveGoAuthFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, "DISPLAY=:0")
	_, stderr, err := f.RunCombinedOutput("auth:browser-login", "--browser", browser+" --new-window")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Regexp(t, `Opened URL: http://127\.0\.0\.1:\d+\nPlease use the browser to log in\.`, stderr)
	b, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Regexp(t, `^--new-window http://127\.0\.0\.1:\d+\n$`, string(b))

	t.Run("not found", func(t *testing.T) {
		f := f.forSubtest(t)
		f.home = t.TempDir()
		l := startBrowserLogin(t, f, "--browser", filepath.Join(dir, "missing")+" --new-window")
		assert.Contains(t, l.stderr.String(), "Please open the following URL in a browser and log in:")
		assert.NotContains(t, l.stderr.String(), "Opened URL")
		l.logIn(t)
		_, _, err := l.wait(t)
		require.NoError(t, err)
	})
}

// TestAuthBrowserLogin_NoInteractionFlag checks that the legacy CLI's -n option disables the login.
func TestAuthBrowserLogin_NoInteractionFlag(t *testing.T) {
	t.Parallel()
	f := newInteractiveGoAuthFactory(t, "", "")
	_, stderr, err := f.RunCombinedOutput("login", "-n")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Non-interactive use of this command is not supported.")
}
