package mockapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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

const (
	invitationPending = "pending"
	roleAdmin         = "admin"
)

// InvitationPermission is an environment type role in a project invitation.
type InvitationPermission struct {
	Type string `json:"type"`
	Role string `json:"role"`
}

type ProjectInvitation struct {
	ID          string
	State       string
	ProjectID   string
	Email       string
	Role        string
	Permissions []InvitationPermission
	CreatedAt   time.Time
}

type OrgInvitation struct {
	ID             string
	State          string
	OrganizationID string
	Email          string
	Permissions    []string
	CreatedAt      time.Time
}

// invitationOwner returns the inviter, as shown in invitations. The caller must hold a lock.
func (h *Handler) invitationOwner() map[string]string {
	return map[string]string{"id": h.myUser.ID, "display_name": h.myUser.Username}
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
			"id":         id,
			"attributes": map[string]string{},
			"_links":     MakeHALLinks("self=" + projectPath + "/environment-types/" + id),
		})
	}
	_ = json.NewEncoder(w).Encode(types)
}

// projectUserAccess is a ProjectUserGrant with links, as returned by the API.
type projectUserAccess struct {
	*ProjectUserGrant
	ProjectTitle string   `json:"project_title"`
	Links        HalLinks `json:"_links,omitempty"`
}

// newProjectUserAccess returns a project user grant as returned by the API. The caller must hold a lock.
func (h *Handler) newProjectUserAccess(g *UserGrant) *projectUserAccess {
	var title string
	if p, ok := h.projects[g.ResourceID]; ok {
		title = p.Title
	}
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
		ProjectTitle: title,
		Links:        MakeHALLinks("self="+path, "update="+path, "delete="+path),
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
	a := h.newProjectUserAccess(h.userGrants[i])
	a.Links["ref:users:0"] = HALLink{HREF: "/ref/users?in=" + url.QueryEscape(a.UserID) + refSig}
	_ = json.NewEncoder(w).Encode(a)
}

func (h *Handler) handlePatchProjectUserAccess(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body.Permissions) == 0 {
		writeProblem(w, http.StatusBadRequest, errors.New("invalid permissions"))
		return
	}
	if msg := validateProjectPermissions(body.Permissions); msg != "" {
		writeProblem(w, http.StatusBadRequest, errors.New("validation failed: "+msg))
		return
	}
	i := h.findProjectGrant(chi.URLParam(req, "project_id"), chi.URLParam(req, "user_id"))
	if i == -1 {
		writeProblem(w, http.StatusNotFound, errors.New("project access not found"))
		return
	}
	patched := *h.userGrants[i]
	patched.Permissions = body.Permissions
	patched.UpdatedAt = time.Now()
	h.userGrants = slices.Clone(h.userGrants)
	h.userGrants[i] = &patched
	w.WriteHeader(http.StatusNoContent)
}

// validateProjectPermissions returns why project permissions are invalid, or an empty string.
func validateProjectPermissions(permissions []string) string {
	isAdmin := slices.Contains(permissions, roleAdmin)
	var hasTypeRole bool
	for _, p := range permissions {
		typ, role, ok := strings.Cut(p, ":")
		switch {
		case !ok && p != roleAdmin && p != "viewer",
			ok && !slices.Contains([]string{"production", "staging", "development"}, typ),
			ok && !slices.Contains([]string{"admin", "contributor", "viewer"}, role):
			return "invalid permission " + p
		case ok && isAdmin && role != roleAdmin:
			return `the "admin" permission must not be paired with a non-admin environment type permission.`
		case ok:
			hasTypeRole = true
		}
	}
	if !isAdmin && !hasTypeRole {
		return "a non-admin user must have at least one environment type permission."
	}
	return ""
}

// handleDeleteProjectUserAccess deletes a grant. Like the API, it succeeds if there is none.
func (h *Handler) handleDeleteProjectUserAccess(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	if i := h.findProjectGrant(chi.URLParam(req, "project_id"), chi.URLParam(req, "user_id")); i != -1 {
		h.userGrants = slices.Delete(slices.Clone(h.userGrants), i, i+1)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCreateProjectInvitation(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	projectID := chi.URLParam(req, "project_id")
	if _, ok := h.projects[projectID]; !ok {
		writeAuthError(w, http.StatusNotFound, "The project does not exist.")
		return
	}
	var body struct {
		Email       string                 `json:"email"`
		Role        string                 `json:"role"`
		Permissions []InvitationPermission `json:"permissions"`
		Force       bool                   `json:"force"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Email == "" ||
		(body.Role != "" && body.Role != roleAdmin && body.Role != "viewer") {
		writeAuthError(w, http.StatusBadRequest, "Invalid input.")
		return
	}
	switch {
	case body.Role != roleAdmin && len(body.Permissions) == 0:
		writeAuthError(w, http.StatusBadRequest,
			"a non-admin user cannot be invited without any environment type permissions")
		return
	case body.Role == roleAdmin && len(body.Permissions) > 0:
		writeAuthError(w, http.StatusBadRequest, "an admin user cannot be invited with environment type permissions")
		return
	}
	pending := slices.IndexFunc(h.projectInvitations[projectID], func(i *ProjectInvitation) bool {
		return i.State == invitationPending && strings.EqualFold(i.Email, body.Email)
	})
	if pending != -1 {
		if !body.Force {
			writeAuthError(w, http.StatusConflict, "A pending invitation already exists for this email address.")
			return
		}
		h.projectInvitations[projectID][pending].State = "cancelled" //nolint:misspell // The API's spelling.
	}
	inv := &ProjectInvitation{
		ID:          lowercaseAlphanumericID(16),
		State:       invitationPending,
		ProjectID:   projectID,
		Email:       body.Email,
		Role:        body.Role,
		Permissions: body.Permissions,
		CreatedAt:   time.Now(),
	}
	if h.projectInvitations == nil {
		h.projectInvitations = make(map[string][]*ProjectInvitation)
	}
	h.projectInvitations[projectID] = append(h.projectInvitations[projectID], inv)
	// Environment type permissions are shown as "environments".
	environments := make([]map[string]string, 0, len(inv.Permissions))
	for _, p := range inv.Permissions {
		environments = append(environments, map[string]string{"type": p.Type, "role": p.Role})
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":           inv.ID,
		"state":        inv.State,
		"project_id":   inv.ProjectID,
		"role":         inv.Role,
		"environments": environments,
		"email":        inv.Email,
		"owner":        h.invitationOwner(),
		"created_at":   inv.CreatedAt,
		"updated_at":   inv.CreatedAt,
		"finished_at":  nil,
	})
}

// orgMember is an OrgMember with its ID and links, as returned by the API.
type orgMember struct {
	*OrgMember
	ID    string   `json:"id"`
	Links HalLinks `json:"_links,omitempty"`
}

func newOrgMember(m *OrgMember) *orgMember {
	path := "/organizations/" + url.PathEscape(m.OrganizationID) + "/members/" + url.PathEscape(m.UserID)
	return &orgMember{OrgMember: m, ID: m.UserID, Links: MakeHALLinks("self="+path, "update="+path, "delete="+path)}
}

// findOrgMember returns the index of an organization member, or -1. The caller must hold a lock.
func (h *Handler) findOrgMember(orgID, userID string) int {
	return slices.IndexFunc(h.orgMembers[orgID], func(m *OrgMember) bool { return m.UserID == userID })
}

// handleListOrgMembers lists an organization's members. Like the API, it sorts by "created_at" or "updated_at"
// ("-" for descending), and paginates with "page[size]" (at most 100) and an opaque "page[after]" cursor.
func (h *Handler) handleListOrgMembers(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	size := 100
	if s := query.Get("page[size]"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			writeProblem(w, http.StatusBadRequest, errors.New("invalid page size"))
			return
		}
		size = n
	}
	var after string
	if c := query.Get("page[after]"); c != "" {
		var ok bool
		if after, ok = decodeCursor(c); !ok {
			writeProblem(w, http.StatusBadRequest, errors.New("invalid cursor"))
			return
		}
	}

	h.RLock()
	defer h.RUnlock()
	orgID := chi.URLParam(req, "organization_id")
	if _, ok := h.orgs[orgID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	members := slices.Clone(h.orgMembers[orgID])
	sort := query.Get("sort")
	switch strings.TrimPrefix(sort, "-") {
	case "":
	case "created_at":
		slices.SortStableFunc(members, func(a, b *OrgMember) int { return timeCompare(a.CreatedAt, b.CreatedAt) })
	case "updated_at":
		slices.SortStableFunc(members, func(a, b *OrgMember) int { return timeCompare(a.UpdatedAt, b.UpdatedAt) })
	default:
		writeProblem(w, http.StatusBadRequest, errors.New("invalid sort field: "+sort))
		return
	}
	if strings.HasPrefix(sort, "-") {
		slices.Reverse(members)
	}
	count := len(members)
	if after != "" {
		i := slices.IndexFunc(members, func(m *OrgMember) bool { return m.UserID == after })
		members = members[i+1:]
	}
	collectionPath := "/organizations/" + url.PathEscape(orgID) + "/members"
	links := MakeHALLinks("self=" + collectionPath)
	if len(members) > size {
		members = members[:size]
		query.Set("page[after]", encodeCursor(members[size-1].UserID))
		links["next"] = HALLink{HREF: collectionPath + "?" + query.Encode()}
	}

	items := make([]*orgMember, 0, len(members))
	userIDs := make([]string, 0, len(members))
	for _, m := range members {
		items = append(items, newOrgMember(m))
		userIDs = append(userIDs, m.UserID)
	}
	links["ref:users:0"] = HALLink{HREF: "/ref/users?in=" + strings.Join(userIDs, ",") + refSig}
	_ = json.NewEncoder(w).Encode(struct {
		Count int          `json:"count"`
		Items []*orgMember `json:"items"`
		Links HalLinks     `json:"_links,omitempty"`
	}{Count: count, Items: items, Links: links})
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
	m.Links["ref:users:0"] = HALLink{HREF: "/ref/users?in=" + url.QueryEscape(m.UserID) + refSig}
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
	if h.orgMembers[orgID][i].Owner {
		writeProblem(w, http.StatusForbidden, errors.New("you cannot update the organization owner"))
		return
	}
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, err)
		return
	}
	if slices.Contains(body.Permissions, roleAdmin) {
		body.Permissions = []string{roleAdmin}
	}
	patched := *h.orgMembers[orgID][i]
	patched.Permissions = body.Permissions
	patched.UpdatedAt = time.Now()
	h.orgMembers[orgID] = slices.Clone(h.orgMembers[orgID])
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
		writeProblem(w, http.StatusForbidden, errors.New("you cannot delete the organization owner"))
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
		writeAuthError(w, http.StatusNotFound, "The organization does not exist.")
		return
	}
	var body struct {
		Email       string   `json:"email"`
		Permissions []string `json:"permissions"`
		Force       bool     `json:"force"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Email == "" || body.Permissions == nil {
		writeAuthError(w, http.StatusBadRequest, "Invalid input.")
		return
	}
	pending := slices.IndexFunc(h.orgInvitations[orgID], func(i *OrgInvitation) bool {
		return i.State == invitationPending && strings.EqualFold(i.Email, body.Email)
	})
	if pending != -1 {
		if !body.Force {
			writeAuthError(w, http.StatusConflict, "A pending invitation already exists for this email address.")
			return
		}
		h.orgInvitations[orgID][pending].State = "cancelled" //nolint:misspell // The API's spelling.
	}
	inv := &OrgInvitation{
		ID:             lowercaseAlphanumericID(16),
		State:          invitationPending,
		OrganizationID: orgID,
		Email:          body.Email,
		Permissions:    body.Permissions,
		CreatedAt:      time.Now(),
	}
	if h.orgInvitations == nil {
		h.orgInvitations = make(map[string][]*OrgInvitation)
	}
	h.orgInvitations[orgID] = append(h.orgInvitations[orgID], inv)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":              inv.ID,
		"state":           inv.State,
		"email":           inv.Email,
		"organization_id": inv.OrganizationID,
		"permissions":     inv.Permissions,
		"owner":           h.invitationOwner(),
		"created_at":      inv.CreatedAt,
		"updated_at":      inv.CreatedAt,
		"finished_at":     nil,
	})
}

// writeAuthError writes an error in the format of the Auth API, which serves invitations and teams.
func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Status  string         `json:"status"`
		Code    int            `json:"code"`
		Title   string         `json:"title"`
		Message string         `json:"message"`
		Detail  map[string]any `json:"detail"`
	}{"Error", status, http.StatusText(status), message, map[string]any{}})
}
