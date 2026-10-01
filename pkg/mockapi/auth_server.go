package mockapi

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

var ValidAPITokens = []string{"api-token-1"}
var accessTokens = []string{"access-token-1"}

// AuthServer is a mock authentication server for testing.
//
// Like the real server, it rotates refresh tokens on every refresh. Reusing a
// rotated refresh token revokes all refresh tokens from the same login.
type AuthServer struct {
	*httptest.Server
	revokedMu     sync.Mutex
	revokedTokens []string

	refreshMu       sync.Mutex
	refreshTokens   map[string]*refreshToken
	refreshDelay    time.Duration
	refreshCount    int
	refreshRequests int
	refreshFailures int
	tokenLifetime   time.Duration
	reuseDetected   bool
	revokedFamilies map[string]bool
}

// SetTokenLifetime sets the lifetime of issued access tokens (default: 1 hour).
func (s *AuthServer) SetTokenLifetime(d time.Duration) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.tokenLifetime = d
}

// SetRefreshFailures makes the next n refresh requests fail with a 503 error.
func (s *AuthServer) SetRefreshFailures(n int) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.refreshFailures = n
}

// RefreshRequests returns the number of refresh_token grant requests received.
func (s *AuthServer) RefreshRequests() int {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshRequests
}

func (s *AuthServer) expiresIn() int {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.tokenLifetime == 0 {
		return 3600
	}
	return int(s.tokenLifetime.Seconds())
}

type refreshToken struct {
	family string
	used   bool
}

// AddRefreshToken makes a refresh token valid, as if it had been issued at login.
func (s *AuthServer) AddRefreshToken(token string) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.refreshTokens[token] = &refreshToken{family: token}
}

// SetRefreshDelay delays refresh token responses, e.g. to make concurrent refreshes overlap.
func (s *AuthServer) SetRefreshDelay(d time.Duration) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.refreshDelay = d
}

// ReuseDetected reports whether a rotated refresh token was sent again.
func (s *AuthServer) ReuseDetected() bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.reuseDetected
}

// issueRefreshToken returns a new refresh token. It starts a new family if family is empty.
// The caller must hold refreshMu.
func (s *AuthServer) issueRefreshToken(family string) string {
	s.refreshCount++
	token := fmt.Sprintf("refresh-token-%d", s.refreshCount)
	if family == "" {
		family = token
	}
	s.refreshTokens[token] = &refreshToken{family: family}
	return token
}

// newRefreshToken is issueRefreshToken for a new login.
func (s *AuthServer) newRefreshToken() string {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.issueRefreshToken("")
}

// rotateRefreshToken exchanges a refresh token for a new one, or returns an OAuth error code.
func (s *AuthServer) rotateRefreshToken(token string) (newToken, errCode string) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	rt, ok := s.refreshTokens[token]
	if !ok || s.revokedFamilies[rt.family] {
		return "", "invalid_grant"
	}
	if rt.used {
		s.reuseDetected = true
		s.revokedFamilies[rt.family] = true
		return "", "invalid_grant"
	}
	rt.used = true
	return s.issueRefreshToken(rt.family), ""
}

// RevokedTokens returns a copy of all tokens that have been revoked.
func (s *AuthServer) RevokedTokens() []string {
	s.revokedMu.Lock()
	defer s.revokedMu.Unlock()
	out := make([]string, len(s.revokedTokens))
	copy(out, s.revokedTokens)
	return out
}

// NewAuthServer creates a new mock authentication server.
// The caller must call Close() on the server when finished.
func NewAuthServer(t *testing.T) *AuthServer {
	mux := chi.NewRouter()
	if testing.Verbose() {
		mux.Use(middleware.DefaultLogger)
	}

	type pendingAuth struct {
		codeChallenge string
		state         string
	}
	var (
		pendingMu    sync.Mutex
		pendingAuths = map[string]pendingAuth{} // code → pendingAuth
	)

	srv := &AuthServer{
		refreshTokens:   map[string]*refreshToken{},
		revokedFamilies: map[string]bool{},
	}

	mux.Get("/oauth2/authorize", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		code := "test-auth-code-" + q.Get("state")
		pendingMu.Lock()
		pendingAuths[code] = pendingAuth{
			codeChallenge: q.Get("code_challenge"),
			state:         q.Get("state"),
		}
		pendingMu.Unlock()
		redirectURI := q.Get("redirect_uri")
		http.Redirect(w, req, redirectURI+"?code="+code+"&state="+q.Get("state"), http.StatusFound)
	})

	mux.Post("/oauth2/token", func(w http.ResponseWriter, req *http.Request) {
		require.NoError(t, req.ParseForm())
		switch req.Form.Get("grant_type") {
		case "api_token":
			apiToken := req.Form.Get("api_token")
			if slices.Contains(ValidAPITokens, apiToken) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  accessTokens[0],
					"expires_in":    srv.expiresIn(),
					"token_type":    "bearer",
					"refresh_token": srv.newRefreshToken(),
				})
				return
			}
			writeOAuthError(w, "request_unauthorized", "The request could not be authorized.")

		case "authorization_code":
			code := req.Form.Get("code")
			verifier := req.Form.Get("code_verifier")
			pendingMu.Lock()
			pending, ok := pendingAuths[code]
			delete(pendingAuths, code)
			pendingMu.Unlock()
			if !ok {
				writeOAuthError(w, "invalid_grant", "The authorization code is invalid.")
				return
			}
			// Verify PKCE S256 challenge.
			h := sha256.Sum256([]byte(verifier))
			expected := base64.RawURLEncoding.EncodeToString(h[:])
			if expected != pending.codeChallenge {
				writeOAuthError(w, "invalid_grant", "The code verifier is invalid.")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  accessTokens[0],
				"expires_in":    srv.expiresIn(),
				"token_type":    "bearer",
				"refresh_token": srv.newRefreshToken(),
			})

		case "refresh_token":
			srv.refreshMu.Lock()
			srv.refreshRequests++
			delay := srv.refreshDelay
			fail := srv.refreshFailures > 0
			if fail {
				srv.refreshFailures--
			}
			srv.refreshMu.Unlock()
			if fail {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			select {
			case <-time.After(delay):
			case <-req.Context().Done():
				// The client went away before the token was rotated.
				return
			}
			newToken, errCode := srv.rotateRefreshToken(req.Form.Get("refresh_token"))
			if errCode != "" {
				writeOAuthError(w, errCode, "The refresh token is invalid.")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  accessTokens[0],
				"expires_in":    srv.expiresIn(),
				"token_type":    "bearer",
				"refresh_token": newToken,
			})

		default:
			writeOAuthError(w, "unsupported_grant_type", "Unsupported grant type: "+req.Form.Get("grant_type"))
		}
	})

	mux.Post("/oauth2/revoke", func(w http.ResponseWriter, req *http.Request) {
		require.NoError(t, req.ParseForm())
		token := req.Form.Get("token")
		srv.revokedMu.Lock()
		srv.revokedTokens = append(srv.revokedTokens, token)
		srv.revokedMu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	mux.Get("/ssh/authority", func(w http.ResponseWriter, _ *http.Request) {
		pks, err := publicKeys()
		require.NoError(t, err)
		data := struct {
			Authorities []string `json:"authorities"`
		}{}
		for _, k := range pks {
			sshPubKey, err := ssh.NewPublicKey(k)
			require.NoError(t, err)
			data.Authorities = append(data.Authorities, string(ssh.MarshalAuthorizedKey(sshPubKey)))
		}
		_ = json.NewEncoder(w).Encode(data)
	})

	mux.Post("/ssh", func(w http.ResponseWriter, req *http.Request) {
		var options struct {
			PublicKey string `json:"key"`
		}
		err := json.NewDecoder(req.Body).Decode(&options)
		require.NoError(t, err)
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(options.PublicKey))
		require.NoError(t, err)
		signer, err := sshSigner()
		require.NoError(t, err)
		extensions := make(map[string]string)

		// Add standard ssh options
		extensions["permit-X11-forwarding"] = ""
		extensions["permit-agent-forwarding"] = ""
		extensions["permit-port-forwarding"] = ""
		extensions["permit-pty"] = ""
		extensions["permit-user-rc"] = ""
		cert := &ssh.Certificate{
			Key:         key,
			Serial:      0,
			CertType:    ssh.UserCert,
			KeyId:       "test-key-id",
			ValidAfter:  uint64(time.Now().Add(-1 * time.Second).Unix()),
			ValidBefore: uint64(time.Now().Add(time.Minute).Unix()),
			Permissions: ssh.Permissions{
				Extensions: extensions,
			},
		}
		err = cert.SignCert(rand.Reader, signer)
		require.NoError(t, err)
		_ = json.NewEncoder(w).Encode(struct {
			Cert string `json:"certificate"`
		}{string(ssh.MarshalAuthorizedKey(cert))})
	})

	srv.Server = httptest.NewServer(mux)
	return srv
}

// writeOAuthError writes an OAuth 2.0 error response (RFC 6749 section 5.2).
func writeOAuthError(w http.ResponseWriter, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}

// publicKeys returns the server's public keys, e.g. for SSH certificate generation.
func publicKeys() ([]crypto.PublicKey, error) {
	pub, _, err := keyPair()
	if err != nil {
		return nil, err
	}

	return []crypto.PublicKey{pub}, nil
}

var (
	privateKey crypto.PrivateKey
	publicKey  crypto.PublicKey
)

func keyPair() (crypto.PublicKey, crypto.PrivateKey, error) {
	if privateKey == nil || publicKey == nil {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		privateKey = priv
		publicKey = pub
	}
	return publicKey, privateKey, nil
}

var signer ssh.Signer

func sshSigner() (ssh.Signer, error) {
	if signer != nil {
		return signer, nil
	}
	_, priv, err := keyPair()
	if err != nil {
		return nil, err
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	signer = s
	return s, nil
}
