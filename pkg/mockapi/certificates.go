package mockapi

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleListProjectCertificates(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	certs := h.projectCertificates[projectID]
	if certs == nil {
		certs = []*Certificate{}
	}
	_ = json.NewEncoder(w).Encode(certs)
}

func (h *Handler) handleGetProjectCertificate(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	certID := chi.URLParam(req, "certificate_id")
	for _, c := range h.projectCertificates[projectID] {
		if c.ID == certID {
			_ = json.NewEncoder(w).Encode(c)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

// certificateInput is the body of a request to add a certificate.
type certificateInput struct {
	Certificate string   `json:"certificate"`
	Key         string   `json:"key"`
	Chain       []string `json:"chain"`
}

// handleCreateProjectCertificate adds a certificate, reading its domains, issuer and expiry from the PEM data.
func (h *Handler) handleCreateProjectCertificate(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	var input certificateInput
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON", nil)
		return
	}
	h.Lock()
	cert, code, message := h.createCertificate(projectID, input)
	h.Unlock()
	if cert == nil {
		writeError(w, code, message, nil)
		return
	}
	// Adding a certificate does not create an activity.
	h.respond(w, "created", http.StatusCreated, cert, nil)
}

// createCertificate adds a certificate, whose ID is its SHA-256 fingerprint, and
// returns a copy, or nil with an error status and message. The caller must hold the write lock.
func (h *Handler) createCertificate(
	projectID string, input certificateInput,
) (cert *Certificate, code int, message string) {
	block, _ := pem.Decode([]byte(input.Certificate))
	if block == nil || input.Key == "" {
		return nil, http.StatusBadRequest, "Invalid certificate or key"
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, http.StatusBadRequest, "Invalid certificate"
	}
	fingerprint := sha256.Sum256(parsed.Raw)
	id := hex.EncodeToString(fingerprint[:])
	if slices.ContainsFunc(h.projectCertificates[projectID], func(c *Certificate) bool { return c.ID == id }) {
		return nil, http.StatusConflict, "The certificate already exists"
	}
	now := time.Now()
	path := "/projects/" + url.PathEscape(projectID) + "/certificates/" + id
	cert = &Certificate{
		ID:          id,
		Certificate: strings.TrimSpace(input.Certificate),
		Chain:       slices.Clone(input.Chain),
		Domains:     parsed.DNSNames,
		AuthType:    []string{},
		Issuer:      []NameAttr{{OID: "2.5.4.3", Alias: "commonName", Value: parsed.Issuer.CommonName}},
		ExpiresAt:   parsed.NotAfter,
		CreatedAt:   now,
		UpdatedAt:   now,
		Links:       MakeHALLinks("self="+path, "#edit="+path, "#delete="+path),
	}
	if cert.Chain == nil {
		cert.Chain = []string{}
	}
	if h.projectCertificates == nil {
		h.projectCertificates = make(map[string][]*Certificate)
	}
	h.projectCertificates[projectID] = append(h.projectCertificates[projectID], cert)
	c := *cert
	return &c, 0, ""
}

// handleDeleteProjectCertificate deletes a certificate, unless it is automatically provisioned.
func (h *Handler) handleDeleteProjectCertificate(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	certID := chi.URLParam(req, "certificate_id")
	h.Lock()
	certs := h.projectCertificates[projectID]
	i := slices.IndexFunc(certs, func(c *Certificate) bool { return c.ID == certID })
	code := http.StatusOK
	switch {
	case i == -1:
		code = http.StatusNotFound
	case certs[i].IsProvisioned:
		code = http.StatusForbidden
	default:
		h.projectCertificates[projectID] = slices.Delete(certs, i, i+1)
	}
	h.Unlock()
	if code != http.StatusOK {
		writeError(w, code, http.StatusText(code), nil)
		return
	}
	// Deleting a certificate does not create an activity.
	h.respond(w, "deleted", http.StatusOK, nil, nil)
}

// ProjectCertificates returns copies of a project's certificates.
func (s *store) ProjectCertificates(projectID string) []*Certificate {
	s.RLock()
	defer s.RUnlock()
	certs := make([]*Certificate, 0, len(s.projectCertificates[projectID]))
	for _, c := range s.projectCertificates[projectID] {
		cp := *c
		certs = append(certs, &cp)
	}
	return certs
}
