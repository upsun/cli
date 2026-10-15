package mockapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
)

// handlePatchProjectSettings updates a project's settings, merging nested objects.
func (h *Handler) handlePatchProjectSettings(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	h.Lock()
	defer h.Unlock()
	p, ok := h.projects[projectID]
	if !ok || p.Settings == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	settings := maps.Clone(p.Settings)
	for k, v := range body {
		current, isMap := settings[k].(map[string]any)
		update, updateIsMap := v.(map[string]any)
		if isMap && updateIsMap {
			merged := maps.Clone(current)
			maps.Copy(merged, update)
			settings[k] = merged
		} else {
			settings[k] = v
		}
	}
	patched := *p
	patched.Settings = settings
	h.projects[projectID] = &patched
	if h.projectSettingsPatches == nil {
		h.projectSettingsPatches = make(map[string][]map[string]any)
	}
	h.projectSettingsPatches[projectID] = append(h.projectSettingsPatches[projectID], body)
	_ = json.NewEncoder(w).Encode(embedded("entity", withSelfLinks(settings, req.URL.Path, true)))
}

// ProjectSettingsPatches returns the bodies of PATCH requests to a project's settings.
func (s *store) ProjectSettingsPatches(projectID string) []map[string]any {
	s.RLock()
	defer s.RUnlock()
	return slices.Clone(s.projectSettingsPatches[projectID])
}

func (h *Handler) handlePatchAutoscalingSettings(w http.ResponseWriter, req *http.Request) {
	env := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if env == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	h.Lock()
	if h.autoscalingPatches == nil {
		h.autoscalingPatches = make(map[string][]map[string]any)
	}
	key := env.Project + "/" + env.ID
	h.autoscalingPatches[key] = append(h.autoscalingPatches[key], body)
	h.Unlock()
	h.RespondWithActivity(w, env.Project, env.ID, "environment.autoscaling.update")
}

// AutoscalingPatches returns the bodies of PATCH requests to an environment's autoscaling settings.
func (s *store) AutoscalingPatches(projectID, environmentID string) []map[string]any {
	s.RLock()
	defer s.RUnlock()
	return slices.Clone(s.autoscalingPatches[projectID+"/"+environmentID])
}
