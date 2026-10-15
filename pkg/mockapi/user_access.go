package mockapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// accessStore holds invitations and organization members. Project user access is in store.userGrants.
type accessStore struct {
	projectInvitations map[string][]*ProjectInvitation
	orgMembers         map[string][]*OrgMember
	orgInvitations     map[string][]*OrgInvitation
}

// InvitationPermission is an environment type role in a project invitation.
type InvitationPermission struct {
	Type string `json:"type"`
	Role string `json:"role"`
}

type ProjectInvitation struct {
	ID          string                 `json:"id"`
	State       string                 `json:"state"`
	ProjectID   string                 `json:"project_id"`
	Email       string                 `json:"email"`
	Role        string                 `json:"role"`
	Permissions []InvitationPermission `json:"permissions"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type OrgInvitation struct {
	ID             string    `json:"id"`
	State          string    `json:"state"`
	OrganizationID string    `json:"organization_id"`
	Email          string    `json:"email"`
	Permissions    []string  `json:"permissions"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// OrgMember is a user in an organization. Its ID is the user ID.
type OrgMember struct {
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Permissions    []string  `json:"permissions"`
	Owner          bool      `json:"owner"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (h *Handler) registerUserAccessRoutes() {
	h.Get("/projects/{project_id}/environment-types", h.handleListEnvironmentTypes)
	h.Get("/projects/{project_id}/user-access/{user_id}", h.handleGetProjectUserAccess)
	h.Patch("/projects/{project_id}/user-access/{user_id}", h.handlePatchProjectUserAccess)
	h.Delete("/projects/{project_id}/user-access/{user_id}", h.handleDeleteProjectUserAccess)
	h.Post("/projects/{project_id}/invitations", h.handleCreateProjectInvitation)

	h.Get("/organizations/{organization_id}/members", h.handleListOrgMembers)
	h.Get("/organizations/{organization_id}/members/{user_id}", h.handleGetOrgMember)
	h.Patch("/organizations/{organization_id}/members/{user_id}", h.handlePatchOrgMember)
	h.Delete("/organizations/{organization_id}/members/{user_id}", h.handleDeleteOrgMember)
	h.Post("/organizations/{organization_id}/invitations", h.handleCreateOrgInvitation)
}

// SetOrgMembers sets the members of an organization.
func (s *store) SetOrgMembers(orgID string, members []*OrgMember) {
	s.Lock()
	defer s.Unlock()
	if s.orgMembers == nil {
		s.orgMembers = make(map[string][]*OrgMember)
	}
	for _, m := range members {
		m.OrganizationID = orgID
	}
	s.orgMembers[orgID] = members
}

// ProjectInvitations returns copies of the invitations created on a project.
func (s *store) ProjectInvitations(projectID string) []ProjectInvitation {
	s.RLock()
	defer s.RUnlock()
	invitations := make([]ProjectInvitation, len(s.projectInvitations[projectID]))
	for i, inv := range s.projectInvitations[projectID] {
		invitations[i] = *inv
	}
	return invitations
}

// OrgInvitations returns copies of the invitations created on an organization.
func (s *store) OrgInvitations(orgID string) []OrgInvitation {
	s.RLock()
	defer s.RUnlock()
	invitations := make([]OrgInvitation, len(s.orgInvitations[orgID]))
	for i, inv := range s.orgInvitations[orgID] {
		invitations[i] = *inv
	}
	return invitations
}

func (h *Handler) handleListEnvironmentTypes(w http.ResponseWriter, req *http.Request) {
	projectPath := "/projects/" + url.PathEscape(chi.URLParam(req, "project_id"))
	types := make([]map[string]any, 0, 3)
	for _, id := range []string{"development", "production", "staging"} {
		types = append(types, map[string]any{
			"id":     id,
			"_links": MakeHALLinks("self=" + projectPath + "/environment-types/" + id),
		})
	}
	_ = json.NewEncoder(w).Encode(types)
}

// projectUserAccess is a ProjectUserGrant with links, as returned by the API.
type projectUserAccess struct {
	*ProjectUserGrant
	Links HalLinks `json:"_links"`
}

func newProjectUserAccess(g *UserGrant) *projectUserAccess {
	path := "/projects/" + url.PathEscape(g.ResourceID) + "/user-access/" + url.PathEscape(g.UserID)
	return &projectUserAccess{
		ProjectUserGrant: &ProjectUserGrant{
			ProjectID:      g.ResourceID,
			OrganizationID: g.OrganizationID,
			UserID:         g.UserID,
			Permissions:    g.Permissions,
			GrantedAt:      g.GrantedAt,
			UpdatedAt:      g.UpdatedAt,
		},
		Links: MakeHALLinks("self="+path, "update="+path, "delete="+path),
	}
}

// findProjectGrant returns the index of a user's grant on a project, or -1. The caller must hold a lock.
func (h *Handler) findProjectGrant(projectID, userID string) int {
	return slices.IndexFunc(h.userGrants, func(g *UserGrant) bool {
		return g.ResourceType == "project" && g.ResourceID == projectID && g.UserID == userID
	})
}

func (h *Handler) handleGetProjectUserAccess(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	i := h.findProjectGrant(chi.URLParam(req, "project_id"), chi.URLParam(req, "user_id"))
	if i == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	a := newProjectUserAccess(h.userGrants[i])
	a.Links["ref:users:0"] = HALLink{HREF: "/ref/users?in=" + url.QueryEscape(a.UserID)}
	_ = json.NewEncoder(w).Encode(a)
}

func (h *Handler) handlePatchProjectUserAccess(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	i := h.findProjectGrant(chi.URLParam(req, "project_id"), chi.URLParam(req, "user_id"))
	if i == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body.Permissions) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	patched := *h.userGrants[i]
	patched.Permissions = body.Permissions
	patched.UpdatedAt = time.Now()
	h.userGrants[i] = &patched
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeleteProjectUserAccess(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	i := h.findProjectGrant(chi.URLParam(req, "project_id"), chi.URLParam(req, "user_id"))
	if i == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.userGrants = slices.Delete(slices.Clone(h.userGrants), i, i+1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCreateProjectInvitation(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	projectID := chi.URLParam(req, "project_id")
	if _, ok := h.projects[projectID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body struct {
		ProjectInvitation
		Force bool `json:"force"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Email == "" || body.Role == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !body.Force && slices.ContainsFunc(h.projectInvitations[projectID], func(i *ProjectInvitation) bool {
		return strings.EqualFold(i.Email, body.Email)
	}) {
		w.WriteHeader(http.StatusConflict)
		return
	}
	inv := body.ProjectInvitation
	inv.ID = lowercaseAlphanumericID(16)
	inv.State = "pending"
	inv.ProjectID = projectID
	inv.CreatedAt = time.Now()
	inv.UpdatedAt = inv.CreatedAt
	if h.projectInvitations == nil {
		h.projectInvitations = make(map[string][]*ProjectInvitation)
	}
	h.projectInvitations[projectID] = append(h.projectInvitations[projectID], &inv)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(&inv)
}

// orgMember is an OrgMember with its ID and links, as returned by the API.
type orgMember struct {
	*OrgMember
	ID    string   `json:"id"`
	Links HalLinks `json:"_links"`
}

func newOrgMember(m *OrgMember) *orgMember {
	path := "/organizations/" + url.PathEscape(m.OrganizationID) + "/members/" + url.PathEscape(m.UserID)
	return &orgMember{OrgMember: m, ID: m.UserID, Links: MakeHALLinks("self="+path, "update="+path, "delete="+path)}
}

// findOrgMember returns the index of an organization member, or -1. The caller must hold a lock.
func (h *Handler) findOrgMember(orgID, userID string) int {
	return slices.IndexFunc(h.orgMembers[orgID], func(m *OrgMember) bool { return m.UserID == userID })
}

func (h *Handler) handleListOrgMembers(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	orgID := chi.URLParam(req, "organization_id")
	if _, ok := h.orgs[orgID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	items := make([]*orgMember, 0, len(h.orgMembers[orgID]))
	userIDs := make([]string, 0, len(h.orgMembers[orgID]))
	for _, m := range h.orgMembers[orgID] {
		items = append(items, newOrgMember(m))
		userIDs = append(userIDs, m.UserID)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Items []*orgMember `json:"items"`
		Links HalLinks     `json:"_links"`
	}{
		Items: items,
		Links: MakeHALLinks(
			"self=/organizations/"+url.PathEscape(orgID)+"/members",
			"ref:users:0=/ref/users?in="+strings.Join(userIDs, ","),
		),
	})
}

func (h *Handler) handleGetOrgMember(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	orgID := chi.URLParam(req, "organization_id")
	i := h.findOrgMember(orgID, chi.URLParam(req, "user_id"))
	if i == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	m := newOrgMember(h.orgMembers[orgID][i])
	m.Links["ref:users:0"] = HALLink{HREF: "/ref/users?in=" + url.QueryEscape(m.UserID)}
	_ = json.NewEncoder(w).Encode(m)
}

func (h *Handler) handlePatchOrgMember(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	orgID := chi.URLParam(req, "organization_id")
	i := h.findOrgMember(orgID, chi.URLParam(req, "user_id"))
	if i == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	patched := *h.orgMembers[orgID][i]
	patched.Permissions = body.Permissions
	patched.UpdatedAt = time.Now()
	h.orgMembers[orgID][i] = &patched
	_ = json.NewEncoder(w).Encode(newOrgMember(&patched))
}

func (h *Handler) handleDeleteOrgMember(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	orgID := chi.URLParam(req, "organization_id")
	i := h.findOrgMember(orgID, chi.URLParam(req, "user_id"))
	if i == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if h.orgMembers[orgID][i].Owner {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "Bad Request", "detail": "The organization owner cannot be removed."})
		return
	}
	h.orgMembers[orgID] = slices.Delete(slices.Clone(h.orgMembers[orgID]), i, i+1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCreateOrgInvitation(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	orgID := chi.URLParam(req, "organization_id")
	if _, ok := h.orgs[orgID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body struct {
		OrgInvitation
		Force bool `json:"force"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Email == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !body.Force && slices.ContainsFunc(h.orgInvitations[orgID], func(i *OrgInvitation) bool {
		return strings.EqualFold(i.Email, body.Email)
	}) {
		w.WriteHeader(http.StatusConflict)
		return
	}
	inv := body.OrgInvitation
	inv.ID = lowercaseAlphanumericID(16)
	inv.State = "pending"
	inv.OrganizationID = orgID
	inv.CreatedAt = time.Now()
	inv.UpdatedAt = inv.CreatedAt
	if h.orgInvitations == nil {
		h.orgInvitations = make(map[string][]*OrgInvitation)
	}
	h.orgInvitations[orgID] = append(h.orgInvitations[orgID], &inv)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(&inv)
}
