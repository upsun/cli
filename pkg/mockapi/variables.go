package mockapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
)

// activityResponse returns a standard activity-embedded response.
func activityResponse() map[string]any {
	return map[string]any{
		"_embedded": map[string]any{"activities": []Activity{}},
	}
}

func (h *Handler) handleListProjectVariables(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	variables := h.projectVariables[projectID]
	// Sort variables in descending order by created date.
	slices.SortFunc(variables, func(a, b *Variable) int { return -timeCompare(a.CreatedAt, b.CreatedAt) })
	_ = json.NewEncoder(w).Encode(variables)
}

func (h *Handler) handleGetProjectVariable(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	variableName, _ := url.PathUnescape(chi.URLParam(req, "name"))
	for _, v := range h.projectVariables[projectID] {
		if variableName == v.Name {
			_ = json.NewEncoder(w).Encode(v)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (h *Handler) handleCreateProjectVariable(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()

	projectID := chi.URLParam(req, "project_id")

	newVar := Variable{}
	if err := json.NewDecoder(req.Body).Decode(&newVar); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	newVar.CreatedAt = time.Now()
	newVar.UpdatedAt = time.Now()
	newVar.Links = MakeHALLinks(
		"self=/projects/"+projectID+"/variables/"+newVar.Name,
		"#edit=/projects/"+projectID+"/variables/"+newVar.Name,
	)

	for _, v := range h.projectVariables[projectID] {
		if newVar.Name == v.Name {
			w.WriteHeader(http.StatusConflict)
			return
		}
	}

	if h.projectVariables[projectID] == nil {
		h.projectVariables = make(map[string][]*Variable)
	}
	h.projectVariables[projectID] = append(h.projectVariables[projectID], &newVar)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"_embedded": map[string]any{"entity": newVar},
	})
}

func (h *Handler) handleDeleteProjectVariable(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	projectID := chi.URLParam(req, "project_id")
	variableName, _ := url.PathUnescape(chi.URLParam(req, "name"))
	for k, v := range h.projectVariables[projectID] {
		if v.Name == variableName {
			h.projectVariables[projectID] = slices.Delete(h.projectVariables[projectID], k, k+1)
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (h *Handler) handlePatchProjectVariable(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()

	projectID := chi.URLParam(req, "project_id")
	variableName, _ := url.PathUnescape(chi.URLParam(req, "name"))
	var key = -1
	for k, v := range h.projectVariables[projectID] {
		if v.Name == variableName {
			key = k
			break
		}
	}
	if key == -1 {
		w.WriteHeader(http.StatusNotFound)
	}
	patched := *h.projectVariables[projectID][key]
	err := json.NewDecoder(req.Body).Decode(&patched)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	patched.UpdatedAt = time.Now()
	h.projectVariables[projectID][key] = &patched
	_ = json.NewEncoder(w).Encode(&patched)
}

func (h *Handler) handleListEnvLevelVariables(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	environmentID, _ := url.PathUnescape(chi.URLParam(req, "environment_id"))
	variables := h.envLevelVariables[projectID][environmentID]
	// Sort variables in descending order by created date.
	slices.SortFunc(variables, func(a, b *EnvLevelVariable) int { return -timeCompare(a.CreatedAt, b.CreatedAt) })
	_ = json.NewEncoder(w).Encode(variables)
}

func (h *Handler) handleGetEnvLevelVariable(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	environmentID, _ := url.PathUnescape(chi.URLParam(req, "environment_id"))
	variableName, _ := url.PathUnescape(chi.URLParam(req, "name"))
	for _, v := range h.envLevelVariables[projectID][environmentID] {
		if variableName == v.Name {
			_ = json.NewEncoder(w).Encode(v)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (h *Handler) handleCreateEnvLevelVariable(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()

	projectID := chi.URLParam(req, "project_id")
	environmentID, _ := url.PathUnescape(chi.URLParam(req, "environment_id"))

	newVar := EnvLevelVariable{}
	if err := json.NewDecoder(req.Body).Decode(&newVar); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	newVar.CreatedAt = time.Now()
	newVar.UpdatedAt = time.Now()
	newVar.Links = MakeHALLinks(
		"self=/projects/"+projectID+"/environments/"+environmentID+"/variables/"+newVar.Name,
		"#edit=/projects/"+projectID+"/environments/"+environmentID+"/variables/"+newVar.Name,
	)

	for _, v := range h.envLevelVariables[projectID][environmentID] {
		if newVar.Name == v.Name {
			w.WriteHeader(http.StatusConflict)
			return
		}
	}

	if h.envLevelVariables == nil {
		h.envLevelVariables = make(map[string]map[string][]*EnvLevelVariable)
	}
	if h.envLevelVariables[projectID] == nil {
		h.envLevelVariables[projectID] = make(map[string][]*EnvLevelVariable)
	}
	h.envLevelVariables[projectID][environmentID] = append(
		h.envLevelVariables[projectID][environmentID],
		&newVar,
	)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"_embedded": map[string]any{"entity": newVar},
	})
}

func (h *Handler) handlePatchEnvLevelVariable(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	projectID := chi.URLParam(req, "project_id")
	environmentID, _ := url.PathUnescape(chi.URLParam(req, "environment_id"))
	variableName, _ := url.PathUnescape(chi.URLParam(req, "name"))
	var key = -1
	for k, v := range h.envLevelVariables[projectID][environmentID] {
		if variableName == v.Name {
			key = k
			break
		}
	}
	if key == -1 {
		w.WriteHeader(http.StatusNotFound)
	}
	patched := *h.envLevelVariables[projectID][environmentID][key]
	err := json.NewDecoder(req.Body).Decode(&patched)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	patched.UpdatedAt = time.Now()
	h.envLevelVariables[projectID][environmentID][key] = &patched
	_ = json.NewEncoder(w).Encode(&patched)
}

func (h *Handler) handleDeleteEnvLevelVariable(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	projectID := chi.URLParam(req, "project_id")
	environmentID, _ := url.PathUnescape(chi.URLParam(req, "environment_id"))
	variableName, _ := url.PathUnescape(chi.URLParam(req, "name"))
	for k, v := range h.envLevelVariables[projectID][environmentID] {
		if v.Name == variableName {
			h.envLevelVariables[projectID][environmentID] = slices.Delete(
				h.envLevelVariables[projectID][environmentID], k, k+1,
			)
			_ = json.NewEncoder(w).Encode(activityResponse())
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}
