package auth

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/upsun/cli/internal/auth/store"
	"github.com/upsun/cli/internal/config"
)

// expiryMargin is how long before its expiry a token counts as expired, so that requests rarely start with a token
// that expires in flight.
const expiryMargin = 2 * time.Minute

// defaultLockWait bounds how long to wait for another process's refresh.
const defaultLockWait = 60 * time.Second

// Token is an access token and its expiry time.
type Token struct {
	AccessToken string `json:"access_token"`
	Expires     int64  `json:"expires,omitempty"` // A Unix timestamp, or 0 if unknown.
}

// Status describes the authentication state, without making any network requests.
type Status struct {
	LoggedIn          bool     `json:"logged_in"`
	SessionIDs        []string `json:"session_ids"`
	HasStoredAPIToken bool     `json:"has_stored_api_token"`
}

// Manager handles tokens and sessions. It is the only component that reads or writes credentials.
type Manager struct {
	Settings *config.Auth
	Store    *store.Store
	OAuth    *OAuthClient

	// Migrator imports credentials from the legacy CLI's storage, once. It may be nil.
	Migrator *Migrator

	// Stderr receives warnings.
	Stderr io.Writer

	// LockWait bounds how long to wait for a lock. It defaults to 60s.
	LockWait time.Duration

	migrateOnce sync.Once
	migrateErr  error
	warnedLocks sync.Once
}

// NewManager creates a Manager from the CLI config.
func NewManager(cnf *config.Config, stderr io.Writer) (*Manager, error) {
	settings, err := cnf.Auth()
	if err != nil {
		return nil, err
	}
	dir, err := cnf.WritableUserDir() //nolint:staticcheck // credentials belong in the user dir, not a cache
	if err != nil {
		return nil, err
	}
	httpClient := NewHTTPClient(cnf, settings)
	return &Manager{
		Settings: settings,
		Store: &store.Store{
			Dir:         filepath.Join(dir, "auth"),
			Service:     cnf.Application.Slug + "-cli-auth",
			UseKeychain: !settings.DisableCredentialHelpers && store.KeychainSupported(),
		},
		OAuth: &OAuthClient{
			HTTPClient: httpClient,
			TokenURL:   settings.TokenURL,
			RevokeURL:  settings.RevokeURL,
			ClientID:   settings.ClientID,
		},
		Stderr: stderr,
	}, nil
}

// NewHTTPClient returns an HTTP client for the API and auth servers, without authentication.
//
// It uses Go's defaults for proxies (HTTPS_PROXY, NO_PROXY) and the system trust store.
func NewHTTPClient(cnf *config.Config, settings *config.Auth) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone() //nolint:errcheck // the default is a *http.Transport
	if settings.SkipSSL {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the user disabled verification
	}
	return &http.Client{Transport: &userAgentTransport{base: t, userAgent: cnf.UserAgent()}}
}

type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", t.userAgent)
	}
	return t.base.RoundTrip(req)
}

// APITokenSessionID returns the session ID used for the OAuth 2.0 tokens obtained from an API token.
//
// This keeps them separate from the user's session, as in the legacy CLI.
func APITokenSessionID(apiToken string) string {
	sum := sha256.Sum256([]byte(apiToken))
	return "api-token-" + hex.EncodeToString(sum[:])[:32]
}

// ConfiguredAPIToken returns an API token set via config (api.token or api.token_file), if any.
func (m *Manager) ConfiguredAPIToken() (string, error) {
	if m.Settings.Token != "" {
		return m.Settings.Token, nil
	}
	if m.Settings.TokenFile != "" {
		b, err := os.ReadFile(m.Settings.TokenFile)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %s", m.Settings.TokenFile)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", nil
}

// HasConfiguredToken reports whether an API token or access token is set via config.
func (m *Manager) HasConfiguredToken() (bool, error) {
	if m.Settings.AccessToken != "" {
		return true, nil
	}
	t, err := m.ConfiguredAPIToken()
	return t != "", err
}

// apiToken returns the API token to use, in order of precedence: a stored one, then one set via config.
func (m *Manager) apiToken(ctx context.Context) (string, error) {
	e, err := m.load(ctx, m.Settings.SessionID)
	if err != nil {
		return "", err
	}
	if e != nil && e.APIToken != "" {
		return e.APIToken, nil
	}
	return m.ConfiguredAPIToken()
}

// Token returns a valid access token, refreshing it if needed.
//
// If rejected is set (an access token the API rejected), and it is still the stored token, the token is refreshed.
func (m *Manager) Token(ctx context.Context, rejected string) (*Token, error) {
	apiToken, err := m.apiToken(ctx)
	if err != nil {
		return nil, err
	}
	if apiToken != "" {
		return m.refresh(ctx, APITokenSessionID(apiToken), rejected, apiToken)
	}
	if m.Settings.AccessToken != "" {
		t := &Token{AccessToken: m.Settings.AccessToken}
		if exp, err := unsafeGetJWTExpiry(t.AccessToken); err == nil {
			t.Expires = exp.Unix()
		}
		return t, nil
	}
	return m.refresh(ctx, m.Settings.SessionID, rejected, "")
}

// Status returns the authentication state.
func (m *Manager) Status(ctx context.Context) (*Status, error) {
	s := &Status{SessionIDs: []string{}}
	ids, err := m.SessionIDs(ctx)
	if err != nil {
		return nil, err
	}
	s.SessionIDs = ids
	e, err := m.load(ctx, m.Settings.SessionID)
	if err != nil {
		return nil, err
	}
	s.HasStoredAPIToken = e != nil && e.APIToken != ""
	hasConfigured, err := m.HasConfiguredToken()
	if err != nil {
		return nil, err
	}
	s.LoggedIn = hasConfigured || (e != nil && (e.AccessToken != "" || e.RefreshToken != "" || e.APIToken != ""))
	return s, nil
}

// SessionIDs lists the user's sessions, excluding the sessions for API tokens.
func (m *Manager) SessionIDs(ctx context.Context) ([]string, error) {
	if err := m.migrate(ctx); err != nil {
		return nil, err
	}
	ids, err := m.Store.List()
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, id := range ids {
		if !strings.HasPrefix(id, "api-token-") {
			out = append(out, id)
		}
	}
	return out, nil
}

// Load returns the stored entry for a session, or nil.
func (m *Manager) Load(ctx context.Context, sessionID string) (*store.Entry, error) {
	return m.load(ctx, sessionID)
}

func (m *Manager) load(ctx context.Context, id string) (*store.Entry, error) {
	if err := m.migrate(ctx); err != nil {
		return nil, err
	}
	return m.Store.Load(id)
}

// Save saves the entry for a session, under the session's lock.
func (m *Manager) Save(ctx context.Context, id string, e *store.Entry) error {
	if err := m.migrate(ctx); err != nil {
		return err
	}
	unlock, err := m.lock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	return m.Store.Save(id, e)
}

// refresh returns a valid token for the session, refreshing it under the session's lock.
//
// Refresh tokens rotate on every refresh, and sending a rotated token again revokes the whole login. So the refresh
// token is always read from the store while the lock is held.
func (m *Manager) refresh(ctx context.Context, id, rejected, apiToken string) (*Token, error) {
	if err := m.migrate(ctx); err != nil {
		return nil, err
	}
	unlock, err := m.lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()

	e, err := m.Store.Load(id)
	if err != nil {
		return nil, err
	}
	if e != nil && usable(e, rejected) {
		return tokenFromEntry(e), nil
	}

	if e != nil && e.RefreshToken != "" {
		newEntry, err := m.OAuth.Refresh(ctx, e.RefreshToken)
		var oerr *OAuthError
		switch {
		case err == nil:
			if newEntry.RefreshToken == "" {
				newEntry.RefreshToken = e.RefreshToken
			}
			// Save the new tokens before doing anything else.
			if err := m.Store.Save(id, newEntry); err != nil {
				return nil, err
			}
			return tokenFromEntry(newEntry), nil
		case errors.As(err, &oerr) && oerr.Code == "invalid_request":
			// Another process sent the same refresh token without the lock. The session is kept.
			return m.afterConcurrentRefresh(ctx, id, e)
		case errors.As(err, &oerr) && oerr.Code == "invalid_grant":
			if err := m.Store.Delete(id); err != nil {
				return nil, err
			}
			if apiToken == "" {
				return nil, loginRequiredAfterRefreshError(oerr)
			}
			// Exchange the API token again below.
		default:
			return nil, fmt.Errorf("failed to refresh the access token: %w", err)
		}
	}

	if apiToken != "" {
		newEntry, err := m.OAuth.ExchangeAPIToken(ctx, apiToken)
		if err != nil {
			var oerr *OAuthError
			if errors.As(err, &oerr) && isInvalidAPITokenError(oerr) {
				return nil, &LoginRequiredError{Notice: "The API token is invalid.", HasAPIToken: true}
			}
			return nil, fmt.Errorf("failed to exchange the API token: %w", err)
		}
		if err := m.Store.Save(id, newEntry); err != nil {
			return nil, err
		}
		return tokenFromEntry(newEntry), nil
	}

	if e != nil && e.AccessToken != "" {
		// An expired or rejected token, with no way to refresh it.
		if err := m.Store.Delete(id); err != nil {
			return nil, err
		}
		return nil, &LoginRequiredError{Notice: "Your session has expired. You have been logged out."}
	}
	return nil, &LoginRequiredError{}
}

// afterConcurrentRefresh waits briefly and uses the stored tokens if another process saved new ones.
func (m *Manager) afterConcurrentRefresh(ctx context.Context, id string, old *store.Entry) (*Token, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Second):
	}
	e, err := m.Store.Load(id)
	if err != nil {
		return nil, err
	}
	if e != nil && e.RefreshToken != old.RefreshToken && e.AccessToken != "" {
		return tokenFromEntry(e), nil
	}
	return nil, errors.New("the access token is being refreshed by another process: please try again")
}

// usable reports whether an entry's access token can be used without refreshing.
func usable(e *store.Entry, rejected string) bool {
	if e.AccessToken == "" || e.AccessToken == rejected {
		return false
	}
	return e.Expires == 0 || time.Unix(e.Expires, 0).After(time.Now().Add(expiryMargin))
}

func tokenFromEntry(e *store.Entry) *Token {
	return &Token{AccessToken: e.AccessToken, Expires: e.Expires}
}

// lock takes the session's lock, bounded by the context and LockWait.
func (m *Manager) lock(ctx context.Context, id string) (unlock func(), err error) {
	mu := sessionMutex(id)
	mu.Lock()
	if m.Settings.DisableLocks {
		m.warnedLocks.Do(func() {
			if m.Stderr != nil {
				fmt.Fprintln(m.Stderr, "Warning: locks are disabled (api.disable_locks). "+
					"Concurrent commands can end the session.")
			}
		})
		return mu.Unlock, nil
	}
	fileUnlock, err := m.fileLock(ctx, m.Store.LockPath(id))
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	return func() {
		fileUnlock()
		mu.Unlock()
	}, nil
}

// fileLock takes an exclusive OS-level lock. The OS releases it if the process dies.
func (m *Manager) fileLock(ctx context.Context, path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	wait := m.LockWait
	if wait == 0 {
		wait = defaultLockWait
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	fl := flock.New(path, flock.SetPermissions(0o600))
	ok, err := fl.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil || !ok {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("timed out waiting for another process to refresh the access token: please try again")
		}
		return nil, fmt.Errorf("failed to lock %s: %w", path, err)
	}
	return func() { _ = fl.Unlock() }, nil
}

var (
	sessionMutexes   = map[string]*sync.Mutex{}
	sessionMutexesMu sync.Mutex
)

// sessionMutex returns a mutex per session ID, so goroutines in one process share one refresh.
func sessionMutex(id string) *sync.Mutex {
	sessionMutexesMu.Lock()
	defer sessionMutexesMu.Unlock()
	mu, ok := sessionMutexes[id]
	if !ok {
		mu = &sync.Mutex{}
		sessionMutexes[id] = mu
	}
	return mu
}

// Logout revokes and deletes a session, including the session for its saved API token.
// If the session is the current one, the session for a configured API token is also logged out.
func (m *Manager) Logout(ctx context.Context, id string) error {
	e, err := m.load(ctx, id)
	if err != nil {
		return err
	}
	var apiTokenSessions []string
	if e != nil && e.APIToken != "" {
		apiTokenSessions = append(apiTokenSessions, APITokenSessionID(e.APIToken))
	}
	if id == m.Settings.SessionID {
		if t, _ := m.ConfiguredAPIToken(); t != "" {
			apiTokenSessions = append(apiTokenSessions, APITokenSessionID(t))
		}
	}
	var errs []error
	for _, sid := range apiTokenSessions {
		errs = append(errs, m.logoutOne(ctx, sid))
	}
	errs = append(errs, m.logoutOne(ctx, id))
	return errors.Join(errs...)
}

// LogoutToReplace logs out of a session before new credentials are saved to it, with apiToken if it is an API token
// login. If the keychain cannot be used, the session files are forgotten instead, so that new ones can be saved.
func (m *Manager) LogoutToReplace(ctx context.Context, id, apiToken string) error {
	err := m.Logout(ctx, id)
	var kerr *store.KeychainError
	if err == nil || !errors.As(err, &kerr) {
		return err
	}
	if m.Stderr != nil {
		fmt.Fprintf(m.Stderr, "Warning: %s\n", err)
	}
	ids := []string{id}
	if apiToken != "" {
		ids = append(ids, APITokenSessionID(apiToken))
	}
	for _, sid := range ids {
		if err := m.Store.Forget(sid); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) logoutOne(ctx context.Context, id string) error {
	unlock, err := m.lock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	e, err := m.Store.Load(id)
	if err != nil {
		return err
	}
	if e != nil {
		for _, r := range []struct{ token, hint string }{
			{e.RefreshToken, "refresh_token"},
			{e.AccessToken, "access_token"},
		} {
			if r.token == "" {
				continue
			}
			if err := m.OAuth.Revoke(ctx, r.token, r.hint); err != nil && m.Stderr != nil {
				fmt.Fprintf(m.Stderr, "Warning: failed to revoke the %s: %s\n", strings.ReplaceAll(r.hint, "_", " "), err)
			}
		}
	}
	return m.Store.Delete(id)
}

// DeleteAll deletes every session's credentials, without revoking them.
func (m *Manager) DeleteAll(ctx context.Context) error {
	if err := m.migrate(ctx); err != nil {
		return err
	}
	return m.Store.DeleteAll()
}

func (m *Manager) migrate(ctx context.Context) error {
	if m.Migrator == nil {
		return nil
	}
	m.migrateOnce.Do(func() {
		m.migrateErr = m.Migrator.Run(ctx, m)
	})
	return m.migrateErr
}
