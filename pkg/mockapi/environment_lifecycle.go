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
	if e := s.lookupEnvironment(projectID, environmentID); e != nil {
		return slices.Clone(e.actions)
	}
	return nil
}

// Environment returns a copy of an environment, or nil if it does not exist.
func (s *store) Environment(projectID, environmentID string) *Environment {
	s.RLock()
	defer s.RUnlock()
	e := s.lookupEnvironment(projectID, environmentID)
	if e == nil {
		return nil
	}
	c := *e
	c.Links = maps.Clone(e.Links)
	c.HTTPAccess = maps.Clone(e.HTTPAccess)
	c.MergeInfo = maps.Clone(e.MergeInfo)
	c.actions = slices.Clone(e.actions)
	return &c
}

// lookupEnvironment finds an environment. The caller must hold a lock.
func (s *store) lookupEnvironment(projectID, environmentID string) *Environment {
	if e := s.environments[environmentID]; e != nil && e.Project == projectID {
		return e
	}
	return nil
}

// decodeBody decodes a JSON request body, if any.
func decodeBody(req *http.Request) map[string]any {
	var body map[string]any
	if b, _ := io.ReadAll(req.Body); len(b) > 0 {
		_ = json.Unmarshal(b, &body)
	}
	return body
}

// recordAction records an operation request on an environment, returning the
// environment, or nil if it does not exist. The caller must hold the write lock.
func (h *Handler) recordAction(req *http.Request, activityType string, body map[string]any) *Environment {
	env := h.lookupEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if env != nil {
		env.actions = append(env.actions, EnvironmentAction{Type: activityType, Body: body})
	}
	return env
}

func (h *Handler) handleBranchEnvironment(w http.ResponseWriter, req *http.Request) {
	body := decodeBody(req)
	name, _ := body["name"].(string)
	title, _ := body["title"].(string)
	envType, _ := body["type"].(string)

	h.Lock()
	parent := h.lookupEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if parent == nil {
		h.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if name == "" {
		h.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if _, exists := h.environments[name]; exists {
		h.Unlock()
		w.WriteHeader(http.StatusConflict)
		return
	}
	h.recordAction(req, "environment.branch", body)
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
	body := decodeBody(req)
	h.Lock()
	env := h.recordAction(req, "environment.deactivate", body)
	if env == nil {
		h.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	env.Status = "inactive"
	env.UpdatedAt = time.Now()
	h.Unlock()
	h.RespondWithActivity(w, env.Project, env.ID, "environment.deactivate")
}

func (h *Handler) handleDeleteEnvironment(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	env := h.lookupEnvironment(chi.URLParam(req, "project_id"), chi.URLParam(req, "environment_id"))
	if env == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
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
