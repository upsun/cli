package mockapi

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"slices"
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

// handleCreateProjectCertificate adds a certificate, reading its domains, issuer and expiry from the PEM data.
func (h *Handler) handleCreateProjectCertificate(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	var body struct {
		Certificate string   `json:"certificate"`
		Key         string   `json:"key"`
		Chain       []string `json:"chain"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	block, _ := pem.Decode([]byte(body.Certificate))
	if block == nil || body.Key == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	now := time.Now()
	id := lowercaseAlphanumericID(16)
	path := "/projects/" + url.PathEscape(projectID) + "/certificates/" + id
	cert := &Certificate{
		ID:        id,
		Domains:   parsed.DNSNames,
		Issuer:    parsed.Issuer.CommonName,
		ExpiresAt: parsed.NotAfter,
		CreatedAt: now,
		UpdatedAt: now,
		Links:     MakeHALLinks("self="+path, "#delete="+path),
	}
	h.Lock()
	if h.projectCertificates == nil {
		h.projectCertificates = make(map[string][]*Certificate)
	}
	h.projectCertificates[projectID] = append(h.projectCertificates[projectID], cert)
	entity := *cert
	h.Unlock()
	w.WriteHeader(http.StatusCreated)
	h.respondWithEntity(w, &entity, &Activity{Type: "project.certificate.create", Project: projectID})
}

// handleDeleteProjectCertificate deletes a certificate, unless it is automatically provisioned.
func (h *Handler) handleDeleteProjectCertificate(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	certID := chi.URLParam(req, "certificate_id")
	h.Lock()
	certs := h.projectCertificates[projectID]
	i := slices.IndexFunc(certs, func(c *Certificate) bool { return c.ID == certID })
	status := http.StatusOK
	switch {
	case i == -1:
		status = http.StatusNotFound
	case certs[i].IsProvisioned:
		status = http.StatusForbidden
	default:
		h.projectCertificates[projectID] = slices.Delete(certs, i, i+1)
	}
	h.Unlock()
	if status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	h.respondWithActivity(w, &Activity{Type: "project.certificate.delete", Project: projectID})
}

// ProjectCertificateIDs returns the IDs of a project's certificates.
func (s *store) ProjectCertificateIDs(projectID string) []string {
	s.RLock()
	defer s.RUnlock()
	ids := make([]string, 0, len(s.projectCertificates[projectID]))
	for _, c := range s.projectCertificates[projectID] {
		ids = append(ids, c.ID)
	}
	return ids
}
