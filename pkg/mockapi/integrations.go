package mockapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleListProjectIntegrations(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	integrations := h.projectIntegrations[projectID]
	if integrations == nil {
		integrations = []*Integration{}
	}
	_ = json.NewEncoder(w).Encode(integrations)
}

func (h *Handler) handleGetProjectIntegration(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	integrationID := chi.URLParam(req, "integration_id")
	for _, i := range h.projectIntegrations[projectID] {
		if i.ID == integrationID {
			_ = json.NewEncoder(w).Encode(i)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}
