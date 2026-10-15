package mockapi

import (
	"cmp"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// EnvironmentAction is a request to an environment operation, e.g. deploy or branch.
type EnvironmentAction struct {
	Type string         // The activity type, e.g. "environment.deploy".
	Body map[string]any // The decoded request body, if any.
}

// AddOperations adds links for environment operations (e.g. "branch" or
// "merge"), which the CLI checks before running them.
func (e *Environment) AddOperations(ops ...string) {
	if e.Links == nil {
		e.Links = make(HalLinks)
	}
	for _, op := range ops {
		e.Links["#"+op] = HALLink{HREF: environmentPath(e.Project, e.ID) + "/" + op}
	}
}

func environmentPath(projectID, environmentID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/environments/" + url.PathEscape(environmentID)
}

// EnvironmentActions returns the operation requests made to an environment.
func (s *store) EnvironmentActions(projectID, environmentID string) []EnvironmentAction {
	s.RLock()
	defer s.RUnlock()
	if e := s.environments[environmentID]; e != nil && e.Project == projectID {
		return slices.Clone(e.actions)
	}
	return nil
}

// Environment returns a copy of an environment, or nil if it does not exist.
func (s *store) Environment(projectID, environmentID string) *Environment {
	s.RLock()
	defer s.RUnlock()
	if e := s.environments[environmentID]; e != nil && e.Project == projectID {
		c := *e
		c.Links = maps.Clone(e.Links)
		c.HTTPAccess = maps.Clone(e.HTTPAccess)
		return &c
	}
	return nil
}

// recordAction records an operation request with its body.
func (h *Handler) recordAction(env *Environment, activityType string, req *http.Request) map[string]any {
	var body map[string]any
	if b, _ := io.ReadAll(req.Body); len(b) > 0 {
		_ = json.Unmarshal(b, &body)
	}
	h.Lock()
	defer h.Unlock()
	env.actions = append(env.actions, EnvironmentAction{Type: activityType, Body: body})
	return body
}

func (h *Handler) handleBranchEnvironment(w http.ResponseWriter, req *http.Request) {
	parent := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if parent == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body := h.recordAction(parent, "environment.branch", req)
	name, _ := body["name"].(string)
	title, _ := body["title"].(string)
	envType, _ := body["type"].(string)
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h.Lock()
	if _, exists := h.environments[name]; exists {
		h.Unlock()
		w.WriteHeader(http.StatusConflict)
		return
	}
	now := time.Now()
	child := &Environment{
		ID:          name,
		Name:        name,
		MachineName: name,
		Title:       cmp.Or(title, name),
		Type:        cmp.Or(envType, "development"),
		Status:      "active",
		Parent:      parent.ID,
		Project:     parent.Project,
		CreatedAt:   now,
		UpdatedAt:   now,
		HeadCommit:  parent.HeadCommit,
		Links:       make(HalLinks),
	}
	// Copy the parent's self and operation links.
	parentPath, childPath := environmentPath(parent.Project, parent.ID), environmentPath(child.Project, child.ID)
	for k, l := range parent.Links {
		if (k == "self" || strings.HasPrefix(k, "#")) && strings.HasPrefix(l.HREF, parentPath) {
			child.Links[k] = HALLink{HREF: childPath + strings.TrimPrefix(l.HREF, parentPath)}
		}
	}
	h.environments[child.ID] = child
	h.Unlock()

	h.respondWithActivity(w, &Activity{
		Type:         "environment.branch",
		Project:      parent.Project,
		Environments: []string{child.ID, parent.ID},
	})
}

func (h *Handler) handleDeactivateEnvironment(w http.ResponseWriter, req *http.Request) {
	env := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if env == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.recordAction(env, "environment.deactivate", req)
	h.Lock()
	env.Status = "inactive"
	env.UpdatedAt = time.Now()
	h.Unlock()
	h.RespondWithActivity(w, env.Project, env.ID, "environment.deactivate")
}

func (h *Handler) handleDeleteEnvironment(w http.ResponseWriter, req *http.Request) {
	env := h.findEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if env == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.Lock()
	defer h.Unlock()
	if env.Status != "inactive" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    http.StatusBadRequest,
			"message": "Only inactive environments can be deleted.",
		})
		return
	}
	delete(h.environments, env.ID)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": http.StatusOK})
}
