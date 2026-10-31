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
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// ValidAPITokens are the API tokens that the server accepts. Each is exchanged for the access token at the same index
// in accessTokens, unless SetUniqueAccessTokens is used.
var ValidAPITokens = []string{"api-token-1", "api-token-2"}
var accessTokens = []string{"access-token-1", "access-token-2"}

// AuthServer is a mock authentication server for testing.
//
// Like the real server, it rotates refresh tokens on every refresh. Reusing a
// rotated refresh token revokes all refresh tokens from the same login.
type AuthServer struct {
	*httptest.Server
	revokedMu      sync.Mutex
	revokedTokens  []string
	revokeFailures int
	revokeStatus   int

	authorizeMu      sync.Mutex
	authorizePrompts []string

	refreshMu       sync.Mutex
	refreshTokens   map[string]*refreshToken
	refreshDelay    time.Duration
	refreshCount    int
	refreshRequests int
	refreshFailures int
	exchanges       int
	tokenLifetime   time.Duration
	uniqueTokens    bool
	jwtTokens       bool
	accessCount     int
	reuseDetected   bool
	revokedFamilies map[string]bool

	sshCertMu       sync.Mutex
	sshCertKeyID    string
	sshCertLifetime time.Duration
	sshCertCount    int
}

// SetSSHCertificate sets the key ID and lifetime of issued SSH certificates (default: "test-key-id" and 1 minute).
// The legacy CLI only reuses a certificate if its key ID is the user ID, and it expires more than 2 minutes later.
func (s *AuthServer) SetSSHCertificate(keyID string, lifetime time.Duration) {
	s.sshCertMu.Lock()
	defer s.sshCertMu.Unlock()
	s.sshCertKeyID = keyID
	s.sshCertLifetime = lifetime
}

// SSHCertificateCount returns the number of SSH certificates issued.
func (s *AuthServer) SSHCertificateCount() int {
	s.sshCertMu.Lock()
	defer s.sshCertMu.Unlock()
	return s.sshCertCount
}

func (s *AuthServer) nextSSHCertificate() (keyID string, lifetime time.Duration) {
	s.sshCertMu.Lock()
	defer s.sshCertMu.Unlock()
	s.sshCertCount++
	keyID, lifetime = s.sshCertKeyID, s.sshCertLifetime
	if keyID == "" {
		keyID = "test-key-id"
	}
	if lifetime == 0 {
		lifetime = time.Minute
	}
	return keyID, lifetime
}

// SetTokenLifetime sets the lifetime of issued access tokens (default: 1 hour).
func (s *AuthServer) SetTokenLifetime(d time.Duration) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.tokenLifetime = d
}

// SetUniqueAccessTokens makes the server issue a new access token each time ("access-token-1", "access-token-2", ...),
// instead of always "access-token-1".
func (s *AuthServer) SetUniqueAccessTokens(unique bool) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.uniqueTokens = unique
}

// SetJWTAccessTokens makes the server issue access tokens as unsigned JWTs, whose "jti" claim is the token that it
// would otherwise issue, and whose "exp" claim is the token's expiry.
func (s *AuthServer) SetJWTAccessTokens(jwt bool) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.jwtTokens = jwt
}

// JWTAccessToken returns an unsigned JWT with the given "jti" and "exp" claims.
func JWTAccessToken(id string, expires time.Time) string {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := enc.EncodeToString(fmt.Appendf(nil, `{"jti":%q,"exp":%d}`, id, expires.Unix()))
	return header + "." + payload + "." + enc.EncodeToString([]byte("signature"))
}

func (s *AuthServer) newAccessToken() string {
	return s.newAccessTokenAt(0)
}

func (s *AuthServer) newAccessTokenAt(i int) string {
	lifetime := time.Duration(s.expiresIn()) * time.Second
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	token := accessTokens[i]
	if s.uniqueTokens {
		s.accessCount++
		token = fmt.Sprintf("access-token-%d", s.accessCount)
	}
	if s.jwtTokens {
		return JWTAccessToken(token, time.Now().Add(lifetime))
	}
	return token
}

// SetRefreshFailures makes the next n refresh requests fail with a 503 error.
func (s *AuthServer) SetRefreshFailures(n int) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.refreshFailures = n
}

// APITokenExchanges returns the number of api_token grant requests received.
func (s *AuthServer) APITokenExchanges() int {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.exchanges
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

// SetRevokeFailures makes the next n revocation requests fail with an HTTP status.
func (s *AuthServer) SetRevokeFailures(n, status int) {
	s.revokedMu.Lock()
	defer s.revokedMu.Unlock()
	s.revokeFailures = n
	s.revokeStatus = status
}

// AuthorizePrompts returns the "prompt" parameter of each authorization request.
func (s *AuthServer) AuthorizePrompts() []string {
	s.authorizeMu.Lock()
	defer s.authorizeMu.Unlock()
	return slices.Clone(s.authorizePrompts)
}

// RevokedTokens returns a copy of all tokens that have been revoked, excluding failed revocations.
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
	mux.Use(RequestLogger(t))

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
		srv.authorizeMu.Lock()
		srv.authorizePrompts = append(srv.authorizePrompts, q.Get("prompt"))
		srv.authorizeMu.Unlock()
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
			srv.refreshMu.Lock()
			srv.exchanges++
			srv.refreshMu.Unlock()
			if i := slices.Index(ValidAPITokens, req.Form.Get("api_token")); i >= 0 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  srv.newAccessTokenAt(i),
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
				"access_token":  srv.newAccessToken(),
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
				"access_token":  srv.newAccessToken(),
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
		defer srv.revokedMu.Unlock()
		if srv.revokeFailures > 0 {
			srv.revokeFailures--
			w.WriteHeader(srv.revokeStatus)
			return
		}
		srv.revokedTokens = append(srv.revokedTokens, token)
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
		keyID, lifetime := srv.nextSSHCertificate()
		cert := &ssh.Certificate{
			Key:         key,
			Serial:      0,
			CertType:    ssh.UserCert,
			KeyId:       keyID,
			ValidAfter:  uint64(time.Now().Add(-1 * time.Second).Unix()),
			ValidBefore: uint64(time.Now().Add(lifetime).Unix()),
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
	priv, err := privateKey()
	if err != nil {
		return nil, err
	}
	return []crypto.PublicKey{priv.Public()}, nil
}

// privateKey returns the server's key, generated once, as servers may run in parallel tests.
var privateKey = sync.OnceValues(func() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	return priv, err
})

func sshSigner() (ssh.Signer, error) {
	priv, err := privateKey()
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}
