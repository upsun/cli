package mockapi

import (
	"cmp"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

func (h *Handler) handleListEnvironments(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	var envs []*Environment
	for _, e := range h.environments {
		if e.Project == projectID {
			envs = append(envs, e)
		}
	}
	// Sort for a deterministic order.
	slices.SortFunc(envs, func(a, b *Environment) int { return cmp.Compare(a.ID, b.ID) })
	_ = json.NewEncoder(w).Encode(envs)
}

func (h *Handler) handleGetEnvironment(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	environmentID := chi.URLParam(req, "environment_id")
	for id, e := range h.environments {
		if e.Project == projectID && id == environmentID {
			_ = json.NewEncoder(w).Encode(e)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (h *Handler) handlePatchEnvironment(w http.ResponseWriter, req *http.Request) {
	env := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if env == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.Lock()
	defer h.Unlock()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	patched := *env
	patched.MergeInfo = maps.Clone(env.MergeInfo)
	var patch struct {
		HTTPAccess map[string]any `json:"http_access"`
	}
	if json.Unmarshal(body, &patched) != nil || json.Unmarshal(body, &patch) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Like the API, http_access is a JSON merge patch, and basic_auth defaults to an empty object.
	patched.HTTPAccess = env.HTTPAccess
	if patch.HTTPAccess != nil {
		patched.HTTPAccess = mergePatch(env.HTTPAccess, patch.HTTPAccess)
		if patched.HTTPAccess["basic_auth"] == nil {
			patched.HTTPAccess["basic_auth"] = map[string]any{}
		}
	}

	patched.UpdatedAt = time.Now()
	h.environments[patched.ID] = &patched
	_ = json.NewEncoder(w).Encode(embedded("entity", &patched))
}

func (h *Handler) handleGetEnvironmentSettings(w http.ResponseWriter, req *http.Request) {
	env := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if env == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.Lock()
	defer h.Unlock()

	settings := make(map[string]any)
	if env.settings != nil {
		settings = env.settings
	}
	// The real API only exposes a "self" link on the settings resource; it does
	// not advertise an "#edit" operation, so the client cannot run an "edit"
	// operation against it.
	settings["_links"] = MakeHALLinks(
		"self=/projects/" + env.Project + "/environments/" + env.ID + "/settings",
	)

	_ = json.NewEncoder(w).Encode(settings)
}

func (h *Handler) handleSetEnvironmentSettings(w http.ResponseWriter, req *http.Request) {
	env := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if env == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.Lock()
	defer h.Unlock()

	settings := make(map[string]any)
	err := json.NewDecoder(req.Body).Decode(&settings)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	for k, v := range settings {
		env.settings[k] = v
	}
	settings["_links"] = MakeHALLinks(
		"self=/projects/" + env.Project + "/environments/" + env.ID + "/settings",
	)

	h.environments[env.ID] = env
	_ = json.NewEncoder(w).Encode(embedded("entity", settings))
}

// handleEnvironmentAction returns a handler for an environment operation (deploy, pause, etc.).
func (h *Handler) handleEnvironmentAction(activityType string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body := decodeBody(req)
		h.Lock()
		env := h.recordAction(req, activityType, body)
		h.Unlock()
		if env == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.RespondWithActivity(w, env.Project, env.ID, activityType)
	}
}

// activityResponse returns a response with an empty list of embedded activities.
func activityResponse() apiResponse {
	return embedded("activities", []Activity{})
}

// embedded returns an accepted response that embeds a value under a key.
func embedded(key string, value any) apiResponse {
	return apiResponse{Status: "OK", Code: http.StatusOK, Embedded: map[string]any{key: value}}
}

func (h *Handler) handleGetCurrentDeployment(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	environmentID := chi.URLParam(req, "environment_id")
	var d *Deployment
	for _, e := range h.environments {
		if e.Project == projectID && e.ID == environmentID {
			d = e.currentDeployment
		}
	}
	if d == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(d)
}

func (h *Handler) handleGetNextDeployment(w http.ResponseWriter, req *http.Request) {
	env := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	h.RLock()
	defer h.RUnlock()
	if env == nil || env.nextDeployment == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(withSelfLinks(env.nextDeployment, req.URL.Path, true))
}

func (h *Handler) handlePatchNextDeployment(w http.ResponseWriter, req *http.Request) {
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
	env.deploymentPatches = append(env.deploymentPatches, body)
	h.Unlock()
	h.RespondWithActivity(w, env.Project, env.ID, "environment.resources.update")
}

// DeploymentPatches returns the bodies of PATCH requests to an environment's next deployment.
func (h *Handler) DeploymentPatches(projectID, environmentID string) []map[string]any {
	env := h.findEnvironment(projectID, environmentID)
	h.RLock()
	defer h.RUnlock()
	if env == nil {
		return nil
	}
	return slices.Clone(env.deploymentPatches)
}

func (h *Handler) handleGetAutoscalingSettings(w http.ResponseWriter, req *http.Request) {
	env := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	h.RLock()
	defer h.RUnlock()
	if env == nil || env.autoscalingSettings == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(withSelfLinks(env.autoscalingSettings, req.URL.Path, false))
}

// withSelfLinks returns a copy of data with a "self" link, and optionally an
// "#edit" link, to the path, unless it already has links.
func withSelfLinks(data map[string]any, path string, edit bool) map[string]any {
	if _, ok := data["_links"]; ok {
		return data
	}
	links := MakeHALLinks("self=" + path)
	if edit {
		links["#edit"] = HALLink{HREF: path}
	}
	result := maps.Clone(data)
	result["_links"] = links
	return result
}

func (h *Handler) handleCreateBackup(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	environmentID := chi.URLParam(req, "environment_id")
	// Like the API, the body is optional, and backups are safe by default.
	var options = struct {
		Safe bool `json:"safe"`
	}{Safe: true}
	if err := json.NewDecoder(req.Body).Decode(&options); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON", nil)
		return
	}
	backup := &Backup{
		ID:            ulid.MustNew(ulid.Now(), rand.Reader).String(),
		EnvironmentID: environmentID,
		Status:        "CREATED",
		Safe:          options.Safe,
		Restorable:    true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	h.addProjectBackup(projectID, backup)
	h.respondWithActivity(w, &Activity{
		Type:         "environment.backup",
		Project:      projectID,
		Environments: []string{environmentID},
		Payload:      map[string]any{"backup_name": backup.ID},
	})
}

func (h *Handler) handleListBackups(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	environmentID := chi.URLParam(req, "environment_id")
	backups := []*Backup{}
	if projectBackups, ok := h.projectBackups[projectID]; ok {
		for _, b := range projectBackups {
			if b.EnvironmentID == environmentID {
				backups = append(backups, backupWithLinks(projectID, b))
			}
		}
	}
	// Sort backups in descending order by created date.
	slices.SortFunc(backups, func(a, b *Backup) int { return -timeCompare(a.CreatedAt, b.CreatedAt) })
	_ = json.NewEncoder(w).Encode(backups)
}

func timeCompare(a, b time.Time) int {
	if a.Equal(b) {
		return 0
	}
	if a.Before(b) {
		return -1
	}
	return 1
}
