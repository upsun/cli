package mockapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
)

// mergePatch returns a copy of data with a patch applied, merging nested objects.
func mergePatch(data, patch map[string]any) map[string]any {
	result := maps.Clone(data)
	if result == nil {
		result = make(map[string]any, len(patch))
	}
	for k, v := range patch {
		current, isMap := result[k].(map[string]any)
		update, updateIsMap := v.(map[string]any)
		if isMap && updateIsMap {
			result[k] = mergePatch(current, update)
		} else {
			result[k] = v
		}
	}
	return result
}

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
	settings := mergePatch(p.Settings, body)
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

// handlePatchAutoscalingSettings updates an environment's autoscaling settings, and records the request body.
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
	env.autoscalingSettings = mergePatch(env.autoscalingSettings, body)
	h.Unlock()
	h.RespondWithActivity(w, env.Project, env.ID, "environment.autoscaling.update")
}

// AutoscalingPatches returns the bodies of PATCH requests to an environment's autoscaling settings.
func (s *store) AutoscalingPatches(projectID, environmentID string) []map[string]any {
	s.RLock()
	defer s.RUnlock()
	return slices.Clone(s.autoscalingPatches[projectID+"/"+environmentID])
}
