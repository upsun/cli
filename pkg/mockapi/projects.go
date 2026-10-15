package mockapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func (h *Handler) handleProjectRefs(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	require.NoError(h.t, req.ParseForm())
	ids := strings.Split(req.Form.Get("in"), ",")
	refs := make(map[string]*ProjectRef, len(ids))
	for _, id := range ids {
		if p, ok := h.projects[id]; ok {
			refs[id] = p.AsRef()
		} else {
			refs[id] = nil
		}
	}
	_ = json.NewEncoder(w).Encode(refs)
}

func (h *Handler) handleGetProject(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	if p, ok := h.projects[projectID]; ok {
		_ = json.NewEncoder(w).Encode(p)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (h *Handler) handleGetProjectSettings(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	p, ok := h.projects[chi.URLParam(req, "project_id")]
	if !ok || p.Settings == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(p.Settings)
}

func (h *Handler) handlePatchProject(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	projectID := chi.URLParam(req, "project_id")
	p, ok := h.projects[projectID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	patched := *p
	err := json.NewDecoder(req.Body).Decode(&patched)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	patched.UpdatedAt = time.Now()
	h.projects[projectID] = &patched
	_ = json.NewEncoder(w).Encode(&patched)
}

func (h *Handler) handleListRegions(w http.ResponseWriter, _ *http.Request) {
	type region struct {
		ID             string `json:"id"`
		Label          string `json:"label"`
		SelectionLabel string `json:"selection_label"`
		Available      bool   `json:"available"`
	}
	type regions struct {
		Regions []region `json:"regions"`
	}
	_ = json.NewEncoder(w).Encode(regions{[]region{{
		ID:             "test-region",
		Label:          "Test Region",
		SelectionLabel: "Test Region",
		Available:      true,
	}}})
}

func (h *Handler) handleProjectUserAccess(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	require.NoError(h.t, req.ParseForm())
	var (
		projectGrants = make([]*projectUserAccess, 0, len(h.userGrants))
		userIDs       = make(uniqueMap)
	)
	for _, g := range h.userGrants {
		if g.ResourceType == "project" && g.ResourceID == projectID {
			projectGrants = append(projectGrants, h.newProjectUserAccess(g))
			userIDs[g.UserID] = struct{}{}
		}
	}
	ret := struct {
		Items []*projectUserAccess `json:"items"`
		Links HalLinks             `json:"_links"`
	}{Items: projectGrants, Links: MakeHALLinks(
		"self=/projects/"+url.PathEscape(projectID)+"/user-access",
		"ref:users:0=/ref/users?in="+strings.Join(userIDs.keys(), ","),
	)}
	_ = json.NewEncoder(w).Encode(ret)
}
