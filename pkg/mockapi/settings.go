package mockapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
)

// mergePatch returns a copy of data with a JSON merge patch (RFC 7386) applied:
// nested objects are merged, and null values delete keys.
func mergePatch(data, patch map[string]any) map[string]any {
	result := maps.Clone(data)
	if result == nil {
		result = make(map[string]any, len(patch))
	}
	for k, v := range patch {
		if v == nil {
			delete(result, k)
			continue
		}
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
	p, ok := h.projects[projectID]
	if !ok || p.Settings == nil {
		h.Unlock()
		writeError(w, http.StatusNotFound, "Not found", nil)
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
	entity := withSelfLinks(settings, req.URL.Path, true)
	h.Unlock()
	h.respond(w, "OK", http.StatusOK, entity, nil)
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
	settings := withSelfLinks(env.autoscalingSettings, req.URL.Path, false)
	h.Unlock()
	// Like the API, the response is the updated settings.
	_ = json.NewEncoder(w).Encode(settings)
}

// AutoscalingPatches returns the bodies of PATCH requests to an environment's autoscaling settings.
func (s *store) AutoscalingPatches(projectID, environmentID string) []map[string]any {
	s.RLock()
	defer s.RUnlock()
	return slices.Clone(s.autoscalingPatches[projectID+"/"+environmentID])
}
