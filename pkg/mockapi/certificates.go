package mockapi

import (
	"encoding/json"
	"net/http"

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
