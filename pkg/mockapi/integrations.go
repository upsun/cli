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
		writeError(w, http.StatusBadRequest, "Invalid integration", map[string]any{"type": "Required"})
		return
	}
	now := time.Now()
	i.ID = lowercaseAlphanumericID(13)
	i.CreatedAt = now
	i.UpdatedAt = now
	path := "/projects/" + url.PathEscape(projectID) + "/integrations/" + i.ID
	i.Links = MakeHALLinks("self="+path, "#edit="+path, "#delete="+path)
	if slices.Contains(validatableIntegrationTypes, i.Type) {
		i.Links["#validate"] = HALLink{HREF: path + "/validate"}
	}
	h.Lock()
	if h.projectIntegrations == nil {
		h.projectIntegrations = make(map[string][]*Integration)
	}
	h.projectIntegrations[projectID] = append(h.projectIntegrations[projectID], &i)
	entity := copyIntegration(&i)
	h.Unlock()
	// Only code source integrations create activities (to fetch code), which are not modeled.
	h.respond(w, "created", http.StatusCreated, entity, nil)
}

func (h *Handler) handlePatchProjectIntegration(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	h.Lock()
	integrations := h.projectIntegrations[projectID]
	idx := slices.IndexFunc(integrations, func(i *Integration) bool { return i.ID == chi.URLParam(req, "integration_id") })
	if idx == -1 {
		h.Unlock()
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	patched := copyIntegration(integrations[idx])
	if err := json.NewDecoder(req.Body).Decode(patched); err != nil {
		h.Unlock()
		writeError(w, http.StatusBadRequest, "Invalid JSON", nil)
		return
	}
	patched.UpdatedAt = time.Now()
	integrations[idx] = patched
	entity := copyIntegration(patched)
	h.Unlock()
	h.respond(w, "OK", http.StatusOK, entity, nil)
}

func (h *Handler) handleDeleteProjectIntegration(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	if !h.deleteProjectIntegration(projectID, chi.URLParam(req, "integration_id")) {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	h.respond(w, "deleted", http.StatusOK, nil, nil)
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

// validatableIntegrationTypes are the integration types with a "#validate" action in the API.
var validatableIntegrationTypes = []string{
	"bitbucket", "bitbucket_server", "cis", "fastly", "github", "gitlab", "health.slack",
	"httplog", "newrelic", "otlp", "splunk", "sumologic", "syslog",
}

// handleValidateProjectIntegration reports that an integration is valid.
func (h *Handler) handleValidateProjectIntegration(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	i := slices.IndexFunc(h.projectIntegrations[chi.URLParam(req, "project_id")], func(i *Integration) bool {
		return i.ID == chi.URLParam(req, "integration_id") && slices.Contains(validatableIntegrationTypes, i.Type)
	})
	h.RUnlock()
	if i == -1 {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	h.respond(w, "OK", http.StatusOK, nil, nil)
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
