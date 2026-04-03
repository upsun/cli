package mockapi

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
type AuthServer struct {
	*httptest.Server
	revokedMu     sync.Mutex
	revokedTokens []string
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

	srv := &AuthServer{}

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
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token":  accessTokens[0],
					"expires_in":    3600,
					"token_type":    "bearer",
					"refresh_token": "test-refresh-token",
				})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid API token"})

		case "authorization_code":
			code := req.Form.Get("code")
			verifier := req.Form.Get("code_verifier")
			pendingMu.Lock()
			pending, ok := pendingAuths[code]
			delete(pendingAuths, code)
			pendingMu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid code"})
				return
			}
			// Verify PKCE S256 challenge.
			h := sha256.Sum256([]byte(verifier))
			expected := base64.RawURLEncoding.EncodeToString(h[:])
			if expected != pending.codeChallenge {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid code_verifier"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  accessTokens[0],
				"expires_in":    3600,
				"token_type":    "bearer",
				"refresh_token": "test-refresh-token",
			})

		case "refresh_token":
			if req.Form.Get("refresh_token") == "test-refresh-token" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token":  accessTokens[0],
					"expires_in":    3600,
					"token_type":    "bearer",
					"refresh_token": "test-refresh-token",
				})
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid refresh token"})

		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid grant type: " + req.Form.Get("grant_type")})
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
			ValidAfter:  uint64(time.Now().Add(-1 * time.Second).Unix()), //nolint:gosec // G115
			ValidBefore: uint64(time.Now().Add(time.Minute).Unix()),      //nolint:gosec // G115
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
