package mockapi

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleListProjectDomains(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	domains := h.projectDomains[projectID]
	if domains == nil {
		domains = []*Domain{}
	}
	_ = json.NewEncoder(w).Encode(domains)
}

func (h *Handler) handleGetProjectDomain(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	domainName, _ := url.PathUnescape(chi.URLParam(req, "name"))
	for _, d := range h.projectDomains[projectID] {
		if d.Name == domainName {
			_ = json.NewEncoder(w).Encode(d)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}
