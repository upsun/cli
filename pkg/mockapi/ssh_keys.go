package mockapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// SSH key sources, reported by /ssh-key-source.
const (
	SSHKeySourceAccounts = "accounts"
	SSHKeySourceAuth     = "auth"
)

// SSHKey is an SSH public key in the current user's account. Keys from the Accounts API have numeric IDs.
type SSHKey struct {
	ID     string
	Label  string
	Value  string
	Active bool
}

// legacySSHKey is a key in the Accounts API's format.
type legacySSHKey struct {
	KeyID       int    `json:"key_id"`
	Title       string `json:"title"`
	Value       string `json:"value"`
	Fingerprint string `json:"fingerprint"`
}

// authSSHKey is a key in the Auth API's format.
type authSSHKey struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	SHA256 string `json:"sha256"`
	Active bool   `json:"active"`
	UserID string `json:"user_id"`
}

// SetSSHKeySource sets the API that owns SSH keys. By default, /ssh-key-source does not exist, so the CLI uses the
// Accounts API.
func (s *store) SetSSHKeySource(source string) {
	s.Lock()
	defer s.Unlock()
	s.sshKeySource = source
}

// SetSSHKeys sets the SSH keys in the current user's account.
func (s *store) SetSSHKeys(keys []*SSHKey) {
	s.Lock()
	defer s.Unlock()
	s.sshKeys = keys
}

// SSHKeys returns copies of the SSH keys in the current user's account.
func (s *store) SSHKeys() []*SSHKey {
	s.RLock()
	defer s.RUnlock()
	keys := make([]*SSHKey, 0, len(s.sshKeys))
	for _, k := range s.sshKeys {
		c := *k
		keys = append(keys, &c)
	}
	return keys
}

func (h *Handler) routeSSHKeys() {
	h.Get("/ssh-key-source", h.handleGetSSHKeySource)

	// The Accounts API, which also lists keys in /me.
	h.Post("/ssh_keys", h.handleCreateSSHKey(SSHKeySourceAccounts))
	h.Get("/ssh_keys/{key_id}", h.handleGetSSHKey(SSHKeySourceAccounts))
	h.Delete("/ssh_keys/{key_id}", h.handleDeleteSSHKey)

	// The Auth API.
	h.Get("/users/{user_id}/ssh-keys", h.handleListSSHKeys)
	h.Post("/users/{user_id}/ssh-keys", h.handleCreateSSHKey(SSHKeySourceAuth))
	h.Get("/users/{user_id}/ssh-keys/{key_id}", h.handleGetSSHKey(SSHKeySourceAuth))
	h.Delete("/users/{user_id}/ssh-keys/{key_id}", h.handleDeleteSSHKey)
}

func (h *Handler) handleGetSSHKeySource(w http.ResponseWriter, _ *http.Request) {
	h.RLock()
	source := h.sshKeySource
	h.RUnlock()
	if source == "" {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"source": source})
}

// handleMe returns the current user in the Accounts API's format, with their SSH keys.
func (h *Handler) handleMe(w http.ResponseWriter, _ *http.Request) {
	h.RLock()
	defer h.RUnlock()
	keys := make([]legacySSHKey, 0, len(h.sshKeys))
	if h.sshKeySource != SSHKeySourceAuth {
		for _, k := range h.sshKeys {
			keys = append(keys, toLegacySSHKey(k))
		}
	}
	_ = json.NewEncoder(w).Encode(struct {
		*User
		SSHKeys []legacySSHKey `json:"ssh_keys"`
		HasKey  bool           `json:"has_key"`
	}{h.myUser, keys, len(keys) > 0})
}

func (h *Handler) handleListSSHKeys(w http.ResponseWriter, _ *http.Request) {
	h.RLock()
	defer h.RUnlock()
	items := make([]authSSHKey, 0, len(h.sshKeys))
	for _, k := range h.sshKeys {
		items = append(items, h.toAuthSSHKey(k))
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "_links": HalLinks{}})
}

func (h *Handler) handleGetSSHKey(source string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		h.RLock()
		defer h.RUnlock()
		i := slices.IndexFunc(h.sshKeys, func(k *SSHKey) bool { return k.ID == chi.URLParam(req, "key_id") })
		if i == -1 {
			writeError(w, http.StatusNotFound, "Not found", nil)
			return
		}
		_ = json.NewEncoder(w).Encode(h.formatSSHKey(source, h.sshKeys[i]))
	}
}

func (h *Handler) handleCreateSSHKey(source string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Value string `json:"value"`
			Title string `json:"title"`
			Label string `json:"label"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Value == "" {
			writeError(w, http.StatusBadRequest, "Invalid SSH key", nil)
			return
		}
		h.Lock()
		defer h.Unlock()
		// Like the API, reject a key that is already registered.
		if slices.ContainsFunc(h.sshKeys, func(k *SSHKey) bool { return k.Value == body.Value }) {
			writeError(w, http.StatusConflict, "The SSH key is already registered", nil)
			return
		}
		k := &SSHKey{Label: body.Title, Value: body.Value, Active: true}
		if source == SSHKeySourceAuth {
			k.ID = lowercaseAlphanumericID(26)
			k.Label = body.Label
		} else {
			k.ID = strconv.Itoa(len(h.sshKeys) + 1)
		}
		h.sshKeys = append(h.sshKeys, k)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(h.formatSSHKey(source, k))
	}
}

func (h *Handler) handleDeleteSSHKey(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	i := slices.IndexFunc(h.sshKeys, func(k *SSHKey) bool { return k.ID == chi.URLParam(req, "key_id") })
	if i == -1 {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	h.sshKeys = slices.Delete(h.sshKeys, i, i+1)
	w.WriteHeader(http.StatusNoContent)
}

// formatSSHKey returns a key in the format of the given source. The caller must hold the lock.
func (h *Handler) formatSSHKey(source string, k *SSHKey) any {
	if source == SSHKeySourceAuth {
		return h.toAuthSSHKey(k)
	}
	return toLegacySSHKey(k)
}

func toLegacySSHKey(k *SSHKey) legacySSHKey {
	id, _ := strconv.Atoi(k.ID)
	// The Accounts API returns an MD5 fingerprint, which the CLI ignores.
	return legacySSHKey{KeyID: id, Title: k.Label, Value: k.Value, Fingerprint: "unused"}
}

func (h *Handler) toAuthSSHKey(k *SSHKey) authSSHKey {
	return authSSHKey{
		ID: k.ID, Label: k.Label, Value: k.Value, SHA256: SSHKeyFingerprint(k.Value), Active: k.Active, UserID: h.myUser.ID,
	}
}

// SSHKeyFingerprint returns the SHA-256 fingerprint of a public key, in the format of `ssh-keygen -l`.
func SSHKeyFingerprint(publicKey string) string {
	fields := strings.Fields(publicKey)
	if len(fields) < 2 {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}
