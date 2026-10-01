package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/auth/store"
	"github.com/upsun/cli/internal/config"
)

// testAuthServer is an OAuth 2.0 token endpoint that rotates refresh tokens and detects reuse.
type testAuthServer struct {
	*httptest.Server
	mu        sync.Mutex
	refreshes int
	issued    int
	valid     map[string]bool // refresh token → unused
	reused    bool
	revoked   []string
	failNext  []int    // status codes to return for the next refresh requests
	errorNext []string // OAuth error codes to return for the next refresh requests
	lifetime  int64
	delay     time.Duration
}

func newTestAuthServer(t *testing.T) *testAuthServer {
	s := &testAuthServer{valid: map[string]bool{}, lifetime: 3600}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.URL.Path == "/revoke" {
			s.mu.Lock()
			s.revoked = append(s.revoked, r.Form.Get("token"))
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		delay := s.delay
		s.mu.Unlock()
		time.Sleep(delay)
		s.mu.Lock()
		defer s.mu.Unlock()
		writeErr := func(status int, code string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": "test error " + code})
		}
		switch r.Form.Get("grant_type") {
		case "refresh_token":
			s.refreshes++
			if len(s.failNext) > 0 {
				status := s.failNext[0]
				s.failNext = s.failNext[1:]
				writeErr(status, "server_error")
				return
			}
			if len(s.errorNext) > 0 {
				code := s.errorNext[0]
				s.errorNext = s.errorNext[1:]
				writeErr(http.StatusBadRequest, code)
				return
			}
			rt := r.Form.Get("refresh_token")
			unused, known := s.valid[rt]
			if !known || !unused {
				if known {
					s.reused = true
				}
				writeErr(http.StatusBadRequest, "invalid_grant")
				return
			}
			s.valid[rt] = false
		case "api_token":
			if r.Form.Get("api_token") != "good-api-token" {
				writeErr(http.StatusBadRequest, "request_unauthorized")
				return
			}
		default:
			writeErr(http.StatusBadRequest, "unsupported_grant_type")
			return
		}
		s.issued++
		newRT := fmt.Sprintf("rt-%d", s.issued)
		s.valid[newRT] = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fmt.Sprintf("at-%d", s.issued),
			"refresh_token": newRT,
			"token_type":    "bearer",
			"expires_in":    s.lifetime,
		})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *testAuthServer) addRefreshToken(rt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.valid[rt] = true
}

func newTestManager(srv *testAuthServer, dir string, settings *config.Auth) *Manager {
	s := *settings
	if s.SessionID == "" {
		s.SessionID = "default"
	}
	return &Manager{
		Settings: &s,
		Store:    &store.Store{Dir: dir},
		OAuth: &OAuthClient{
			HTTPClient: srv.Client(),
			TokenURL:   srv.URL + "/token",
			RevokeURL:  srv.URL + "/revoke",
			ClientID:   "test",
			retryDelay: time.Millisecond,
		},
	}
}

func TestManager_Token(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()
	cases := []struct {
		name          string
		entry         *store.Entry
		settings      config.Auth
		rejected      string
		setup         func(s *testAuthServer)
		wantToken     string
		wantRefreshes int
		wantLogin     string // The expected notice of a login-required error.
		wantErr       string
		wantDeleted   bool
	}{
		{
			name:      "valid token",
			entry:     &store.Entry{AccessToken: "stored", RefreshToken: "rt-0", Expires: future},
			wantToken: "stored",
		},
		{
			name:          "expired token",
			entry:         &store.Entry{AccessToken: "stored", RefreshToken: "rt-0", Expires: past},
			wantToken:     "at-1",
			wantRefreshes: 1,
		},
		{
			name: "inside the expiry margin",
			entry: &store.Entry{
				AccessToken: "stored", RefreshToken: "rt-0", Expires: time.Now().Add(time.Minute).Unix(),
			},
			wantToken:     "at-1",
			wantRefreshes: 1,
		},
		{
			name:          "rejected token",
			entry:         &store.Entry{AccessToken: "stored", RefreshToken: "rt-0", Expires: future},
			rejected:      "stored",
			wantToken:     "at-1",
			wantRefreshes: 1,
		},
		{
			name:      "rejected token already replaced",
			entry:     &store.Entry{AccessToken: "newer", RefreshToken: "rt-0", Expires: future},
			rejected:  "older",
			wantToken: "newer",
		},
		{
			name:      "not logged in",
			wantLogin: "",
		},
		{
			name:          "invalid grant",
			entry:         &store.Entry{AccessToken: "stored", RefreshToken: "unknown", Expires: past},
			wantRefreshes: 1,
			wantLogin:     "Your session has expired. You have been logged out.",
			wantDeleted:   true,
		},
		{
			name:        "expired without refresh token",
			entry:       &store.Entry{AccessToken: "stored", Expires: past},
			wantLogin:   "Your session has expired. You have been logged out.",
			wantDeleted: true,
		},
		{
			name:          "transient 5xx is retried once",
			entry:         &store.Entry{AccessToken: "stored", RefreshToken: "rt-0", Expires: past},
			setup:         func(s *testAuthServer) { s.failNext = []int{503} },
			wantToken:     "at-1",
			wantRefreshes: 2,
		},
		{
			name:          "repeated 5xx keeps the session",
			entry:         &store.Entry{AccessToken: "stored", RefreshToken: "rt-0", Expires: past},
			setup:         func(s *testAuthServer) { s.failNext = []int{503, 503} },
			wantRefreshes: 2,
			wantErr:       "failed to refresh the access token",
		},
		{
			name:          "concurrent use keeps the session",
			entry:         &store.Entry{AccessToken: "stored", RefreshToken: "rt-0", Expires: past},
			setup:         func(s *testAuthServer) { s.errorNext = []string{"invalid_request"} },
			wantRefreshes: 1,
			wantErr:       "being refreshed by another process",
		},
		{
			name:      "access token from config",
			settings:  config.Auth{AccessToken: "raw"},
			wantToken: "raw",
		},
		{
			name:      "API token from config",
			settings:  config.Auth{Token: "good-api-token"},
			wantToken: "at-1",
		},
		{
			name:      "invalid API token",
			settings:  config.Auth{Token: "bad-api-token"},
			wantLogin: "The API token is invalid.",
		},
		{
			name:      "stored API token takes precedence",
			entry:     &store.Entry{APIToken: "good-api-token"},
			settings:  config.Auth{Token: "bad-api-token"},
			wantToken: "at-1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newTestAuthServer(t)
			srv.addRefreshToken("rt-0")
			if c.setup != nil {
				c.setup(srv)
			}
			m := newTestManager(srv, t.TempDir(), &c.settings)
			if c.entry != nil {
				require.NoError(t, m.Store.Save("default", c.entry))
			}

			tok, err := m.Token(context.Background(), c.rejected)
			switch {
			case c.wantErr != "":
				assert.ErrorContains(t, err, c.wantErr)
			case c.wantToken == "":
				lerr, ok := AsLoginRequired(err)
				require.True(t, ok, "expected a login-required error, got: %v", err)
				assert.Equal(t, c.wantLogin, lerr.Notice)
			default:
				require.NoError(t, err)
				assert.Equal(t, c.wantToken, tok.AccessToken)
			}
			assert.Equal(t, c.wantRefreshes, srv.refreshes)
			assert.False(t, srv.reused)

			stored, err := m.Store.Load("default")
			require.NoError(t, err)
			if c.wantDeleted {
				assert.Nil(t, stored)
			} else if c.entry != nil && c.entry.RefreshToken != "" && c.wantErr != "" {
				assert.Equal(t, c.entry.RefreshToken, stored.RefreshToken, "the session must be kept")
			}
		})
	}
}

// TestManager_ConcurrentRefresh simulates several processes, each with its own Manager, refreshing one session.
func TestManager_ConcurrentRefresh(t *testing.T) {
	srv := newTestAuthServer(t)
	srv.addRefreshToken("rt-0")
	srv.delay = 50 * time.Millisecond
	dir := t.TempDir()
	require.NoError(t, newTestManager(srv, dir, &config.Auth{}).Store.Save("default", &store.Entry{
		AccessToken: "expired", RefreshToken: "rt-0", Expires: time.Now().Add(-time.Hour).Unix(),
	}))

	const n = 10
	var wg sync.WaitGroup
	tokens := make([]string, n)
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			m := newTestManager(srv, dir, &config.Auth{})
			tok, err := m.Token(context.Background(), "")
			errs[i] = err
			if tok != nil {
				tokens[i] = tok.AccessToken
			}
		})
	}
	wg.Wait()
	for i := range n {
		require.NoError(t, errs[i])
		assert.Equal(t, "at-1", tokens[i])
	}
	assert.Equal(t, 1, srv.refreshes)
	assert.False(t, srv.reused)
}

func TestManager_LockTimeout(t *testing.T) {
	srv := newTestAuthServer(t)
	dir := t.TempDir()
	m := newTestManager(srv, dir, &config.Auth{})
	m.LockWait = 100 * time.Millisecond
	require.NoError(t, m.Store.Save("default", &store.Entry{
		AccessToken: "expired", RefreshToken: "rt-0", Expires: time.Now().Add(-time.Hour).Unix(),
	}))

	// Another "process" holds the lock.
	other := newTestManager(srv, dir, &config.Auth{})
	unlock, err := other.fileLock(context.Background(), m.Store.LockPath("default"))
	require.NoError(t, err)
	defer unlock()

	_, err = m.Token(context.Background(), "")
	assert.ErrorContains(t, err, "timed out waiting")
	assert.Equal(t, 0, srv.refreshes, "the token must never be refreshed without the lock")
}

func TestManager_LogoutAndStatus(t *testing.T) {
	srv := newTestAuthServer(t)
	m := newTestManager(srv, t.TempDir(), &config.Auth{})
	ctx := context.Background()

	s, err := m.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, &Status{SessionIDs: []string{}}, s)

	require.NoError(t, m.Store.Save("default", &store.Entry{
		AccessToken: "at", RefreshToken: "rt", APIToken: "good-api-token",
	}))
	require.NoError(t, m.Store.Save(APITokenSessionID("good-api-token"), &store.Entry{AccessToken: "api-at"}))
	require.NoError(t, m.Store.Save("other", &store.Entry{AccessToken: "other-at"}))

	s, err = m.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, &Status{LoggedIn: true, SessionIDs: []string{"default", "other"}, HasStoredAPIToken: true}, s)

	require.NoError(t, m.Logout(ctx, "default"))
	assert.ElementsMatch(t, []string{"rt", "at", "api-at"}, srv.revoked)
	ids, err := m.Store.List()
	require.NoError(t, err)
	assert.Equal(t, []string{"other"}, ids)
}

func TestMigrator(t *testing.T) {
	srv := newTestAuthServer(t)
	dir := t.TempDir()
	markerPath := filepath.Join(dir, store.MigrationMarker)
	exported := `{
		"default": {"access_token": "a", "refresh_token": "r", "token_type": "bearer", "expires": 123},
		"other": {"api_token": "t"},
		"api-token-abc": {"access_token": "skipped"},
		"empty": {}
	}`
	var exports, deletes int
	failExport, failDelete := true, true
	mg := &Migrator{Export: func(_ context.Context, del bool) ([]byte, error) {
		if del {
			deletes++
			if failDelete {
				return nil, fmt.Errorf("delete failed")
			}
			return nil, nil
		}
		exports++
		if failExport {
			return nil, fmt.Errorf("export failed")
		}
		return []byte(exported), nil
	}}
	var stderr strings.Builder
	newManager := func() *Manager {
		m := newTestManager(srv, dir, &config.Auth{})
		m.Migrator = mg
		m.Stderr = &stderr
		return m
	}
	// expireRetry makes a failed step due for a retry.
	expireRetry := func() {
		b, err := os.ReadFile(markerPath)
		require.NoError(t, err)
		var mk migrationMarker
		require.NoError(t, json.Unmarshal(b, &mk))
		assert.Greater(t, mk.RetryAfter, time.Now().Unix())
		mk.RetryAfter = 0
		require.NoError(t, writeMarker(markerPath, &mk))
	}

	// A failed export does not block authentication, and is retried later.
	ids, err := newManager().SessionIDs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, ids)
	assert.Contains(t, stderr.String(), "failed to migrate credentials from the legacy CLI: export failed")
	_, err = newManager().SessionIDs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, exports, "the export must not be retried immediately")

	failExport = false
	expireRetry()
	ids, err = newManager().SessionIDs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"default", "other"}, ids)
	e, err := newManager().Load(context.Background(), "default")
	require.NoError(t, err)
	assert.Equal(t, &store.Entry{AccessToken: "a", RefreshToken: "r", TokenType: "bearer", Expires: 123}, e)
	assert.Equal(t, 2, exports)
	assert.Equal(t, 1, deletes, "a failed delete must not be retried immediately")

	failDelete = false
	expireRetry()
	_, err = newManager().SessionIDs(context.Background())
	require.NoError(t, err)
	_, err = newManager().SessionIDs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, exports, "sessions must only be imported once")
	assert.Equal(t, 2, deletes)

	b, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(b))
}

func TestTransport(t *testing.T) {
	srv := newTestAuthServer(t)
	srv.addRefreshToken("rt-0")
	m := newTestManager(srv, t.TempDir(), &config.Auth{})
	require.NoError(t, m.Store.Save("default", &store.Entry{
		AccessToken: "revoked", RefreshToken: "rt-0", Expires: time.Now().Add(time.Hour).Unix(),
	}))

	var authHeaders []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/step-up":
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_user_authentication"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"amr": ["mfa"], "max_age": 60}`))
		case r.Header.Get("Authorization") != "Bearer at-1":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer api.Close()

	client := NewClient(m, nil)
	resp, err := client.Get(api.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []string{"Bearer revoked", "Bearer at-1"}, authHeaders)

	_, err = client.Get(api.URL + "/step-up") //nolint:bodyclose // the request fails
	lerr, ok := AsLoginRequired(err)
	require.True(t, ok)
	assert.Equal(t, []string{"mfa"}, lerr.AuthMethods)
	assert.Equal(t, 60, *lerr.MaxAge)
	assert.Equal(t, "Multi-factor authentication (MFA) is required.", lerr.Message())
}
