package mockapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"time"

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

func (h *Handler) handleCreateProjectIntegration(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	var i Integration
	if err := json.NewDecoder(req.Body).Decode(&i); err != nil || i.Type == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	now := time.Now()
	i.ID = lowercaseAlphanumericID(13)
	i.CreatedAt = now
	i.UpdatedAt = now
	path := "/projects/" + url.PathEscape(projectID) + "/integrations/" + i.ID
	i.Links = MakeHALLinks("self="+path, "#edit="+path, "#delete="+path, "#validate="+path+"/validate")
	h.Lock()
	if h.projectIntegrations == nil {
		h.projectIntegrations = make(map[string][]*Integration)
	}
	h.projectIntegrations[projectID] = append(h.projectIntegrations[projectID], &i)
	entity := copyIntegration(&i)
	h.Unlock()
	w.WriteHeader(http.StatusCreated)
	h.respondWithEntity(w, entity, &Activity{Type: "integration.create", Project: projectID})
}

func (h *Handler) handlePatchProjectIntegration(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	h.Lock()
	integrations := h.projectIntegrations[projectID]
	idx := slices.IndexFunc(integrations, func(i *Integration) bool { return i.ID == chi.URLParam(req, "integration_id") })
	if idx == -1 {
		h.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	patched := copyIntegration(integrations[idx])
	if err := json.NewDecoder(req.Body).Decode(patched); err != nil {
		h.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	patched.UpdatedAt = time.Now()
	integrations[idx] = patched
	entity := copyIntegration(patched)
	h.Unlock()
	h.respondWithEntity(w, entity, &Activity{Type: "integration.update", Project: projectID})
}

func (h *Handler) handleDeleteProjectIntegration(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	if !h.deleteProjectIntegration(projectID, chi.URLParam(req, "integration_id")) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.respondWithActivity(w, &Activity{Type: "integration.delete", Project: projectID})
}

// deleteProjectIntegration deletes an integration, and reports whether it existed.
func (h *Handler) deleteProjectIntegration(projectID, integrationID string) bool {
	h.Lock()
	defer h.Unlock()
	integrations := h.projectIntegrations[projectID]
	idx := slices.IndexFunc(integrations, func(i *Integration) bool { return i.ID == integrationID })
	if idx == -1 {
		return false
	}
	h.projectIntegrations[projectID] = slices.Delete(integrations, idx, idx+1)
	return true
}

// handleValidateProjectIntegration reports that an integration is valid.
func (h *Handler) handleValidateProjectIntegration(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	if !slices.ContainsFunc(h.projectIntegrations[projectID], func(i *Integration) bool {
		return i.ID == chi.URLParam(req, "integration_id")
	}) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

func copyIntegration(i *Integration) *Integration {
	c := *i
	c.Events = slices.Clone(i.Events)
	c.Links = maps.Clone(i.Links)
	return &c
}

// Integrations returns copies of a project's integrations.
func (s *store) Integrations(projectID string) []*Integration {
	s.RLock()
	defer s.RUnlock()
	result := make([]*Integration, 0, len(s.projectIntegrations[projectID]))
	for _, i := range s.projectIntegrations[projectID] {
		result = append(result, copyIntegration(i))
	}
	return result
}
