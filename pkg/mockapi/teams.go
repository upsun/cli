package mockapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// teamStore holds teams, their members and their project access.
type teamStore struct {
	teams        map[string]*Team
	teamMembers  map[string][]*TeamMember
	teamProjects map[string][]*TeamProjectAccess
}

type Team struct {
	ID                 string     `json:"id"`
	OrganizationID     string     `json:"organization_id"`
	Label              string     `json:"label"`
	ProjectPermissions []string   `json:"project_permissions"`
	Counts             TeamCounts `json:"counts"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Links              HalLinks   `json:"_links,omitempty"`
}

// TeamCounts is calculated from the team's members and projects.
type TeamCounts struct {
	MemberCount  int `json:"member_count"`
	ProjectCount int `json:"project_count"`
}

type TeamMember struct {
	TeamID    string    `json:"team_id"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Links     HalLinks  `json:"_links,omitempty"`
}

type TeamProjectAccess struct {
	TeamID         string    `json:"team_id"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      string    `json:"project_id"`
	ProjectTitle   string    `json:"project_title"`
	GrantedAt      time.Time `json:"granted_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Links          HalLinks  `json:"_links,omitempty"`
}

// initTeams initializes the maps. The caller must hold the write lock.
func (s *teamStore) initTeams() {
	if s.teams == nil {
		s.teams = make(map[string]*Team)
		s.teamMembers = make(map[string][]*TeamMember)
		s.teamProjects = make(map[string][]*TeamProjectAccess)
	}
}

func (h *Handler) registerTeamRoutes() {
	h.Get("/teams", h.handleListTeams)
	h.Post("/teams", h.handleCreateTeam)
	h.Get("/teams/{team_id}", h.handleGetTeam)
	h.Patch("/teams/{team_id}", h.handlePatchTeam)
	h.Delete("/teams/{team_id}", h.handleDeleteTeam)
	h.Get("/teams/{team_id}/members", h.handleListTeamMembers)
	h.Post("/teams/{team_id}/members", h.handleAddTeamMember)
	h.Get("/teams/{team_id}/members/{user_id}", h.handleGetTeamMember)
	h.Delete("/teams/{team_id}/members/{user_id}", h.handleDeleteTeamMember)
	h.Get("/teams/{team_id}/project-access", h.handleListTeamProjects)
	h.Post("/teams/{team_id}/project-access", h.handleAddTeamProjects)
	h.Delete("/teams/{team_id}/project-access/{project_id}", h.handleDeleteTeamProject)
	h.Get("/projects/{project_id}/team-access", h.handleListProjectTeamAccess)
}

// SetTeams sets the teams, replacing any existing teams, members and project access.
func (s *store) SetTeams(teams []*Team) {
	s.Lock()
	defer s.Unlock()
	s.teams = make(map[string]*Team, len(teams))
	s.teamMembers = make(map[string][]*TeamMember)
	s.teamProjects = make(map[string][]*TeamProjectAccess)
	for _, t := range teams {
		s.teams[t.ID] = t
	}
}

// SetTeamMembers sets the users in a team.
func (s *store) SetTeamMembers(teamID string, members []*TeamMember) {
	s.Lock()
	defer s.Unlock()
	s.initTeams()
	for _, m := range members {
		m.TeamID = teamID
	}
	s.teamMembers[teamID] = members
}

// SetTeamProjects sets the projects a team can access.
func (s *store) SetTeamProjects(teamID string, projects []*TeamProjectAccess) {
	s.Lock()
	defer s.Unlock()
	s.initTeams()
	for _, p := range projects {
		p.TeamID = teamID
	}
	s.teamProjects[teamID] = projects
}

// teamResponse returns a copy of a team with its counts and links. The caller must hold a lock.
func (h *Handler) teamResponse(t *Team, withinCollection bool) *Team {
	c := *t
	c.Counts = TeamCounts{MemberCount: len(h.teamMembers[t.ID]), ProjectCount: len(h.teamProjects[t.ID])}
	c.Links = MakeHALLinks("self=/teams/" + url.PathEscape(t.ID))
	if !withinCollection {
		c.Links["ref:organizations:0"] = HALLink{HREF: "/ref/organizations?in=" + url.QueryEscape(t.OrganizationID) + refSig}
	}
	return &c
}

// handleListTeams lists teams. Like the API, it sorts by created_at by default.
func (h *Handler) handleListTeams(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	orgID := req.URL.Query().Get("filter[organization_id]")
	var ids []string
	if in := req.URL.Query().Get("filter[id][in]"); in != "" {
		ids = strings.Split(in, ",")
	}
	items := make([]*Team, 0, len(h.teams))
	orgIDs := make(uniqueMap)
	for _, t := range h.teams {
		if (orgID == "" || t.OrganizationID == orgID) && (ids == nil || slices.Contains(ids, t.ID)) {
			items = append(items, h.teamResponse(t, true))
			orgIDs[t.OrganizationID] = struct{}{}
		}
	}
	slices.SortFunc(items, func(a, b *Team) int { return strings.Compare(a.ID, b.ID) })
	sort := req.URL.Query().Get("sort")
	switch strings.TrimPrefix(sort, "-") {
	case "label":
		slices.SortStableFunc(items, func(a, b *Team) int { return strings.Compare(a.Label, b.Label) })
	case "", "created_at":
		slices.SortStableFunc(items, func(a, b *Team) int { return timeCompare(a.CreatedAt, b.CreatedAt) })
	case "updated_at":
		slices.SortStableFunc(items, func(a, b *Team) int { return timeCompare(a.UpdatedAt, b.UpdatedAt) })
	default:
		writeAuthError(w, http.StatusBadRequest, "can't sort on "+sort)
		return
	}
	if strings.HasPrefix(sort, "-") {
		slices.Reverse(items)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Items []*Team  `json:"items"`
		Count int      `json:"count"`
		Links HalLinks `json:"_links,omitempty"`
	}{Items: items, Count: len(items), Links: MakeHALLinks(
		"self=/teams",
		"ref:organizations:0=/ref/organizations?in="+strings.Join(orgIDs.keys(), ",")+refSig,
	)})
}

// handleCreateTeam creates a team. Like the API, it does not require a unique label.
func (h *Handler) handleCreateTeam(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	var t Team
	if err := json.NewDecoder(req.Body).Decode(&t); err != nil || t.Label == "" {
		writeAuthError(w, http.StatusBadRequest, "Invalid input.")
		return
	}
	if _, ok := h.orgs[t.OrganizationID]; !ok {
		writeAuthError(w, http.StatusBadRequest, "The organization does not exist.")
		return
	}
	t.ID = lowercaseAlphanumericID(26)
	t.CreatedAt = time.Now()
	t.UpdatedAt = t.CreatedAt
	if t.ProjectPermissions == nil {
		t.ProjectPermissions = []string{}
	}
	h.initTeams()
	h.teams[t.ID] = &t
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(h.teamResponse(&t, false))
}

func (h *Handler) handleGetTeam(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	t, ok := h.teams[chi.URLParam(req, "team_id")]
	if !ok {
		writeAuthError(w, http.StatusNotFound, "Requested team does not exist.")
		return
	}
	_ = json.NewEncoder(w).Encode(h.teamResponse(t, false))
}

func (h *Handler) handlePatchTeam(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	t, ok := h.teams[chi.URLParam(req, "team_id")]
	if !ok {
		writeAuthError(w, http.StatusNotFound, "Requested team does not exist.")
		return
	}
	var body struct {
		Label              *string  `json:"label"`
		ProjectPermissions []string `json:"project_permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeAuthError(w, http.StatusBadRequest, "Invalid input.")
		return
	}
	patched := *t
	if body.Label != nil {
		patched.Label = *body.Label
	}
	if body.ProjectPermissions != nil {
		patched.ProjectPermissions = body.ProjectPermissions
	}
	patched.UpdatedAt = time.Now()
	h.teams[t.ID] = &patched
	_ = json.NewEncoder(w).Encode(h.teamResponse(&patched, false))
}

func (h *Handler) handleDeleteTeam(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	if _, ok := h.teams[teamID]; !ok {
		writeAuthError(w, http.StatusNotFound, "Requested team does not exist.")
		return
	}
	delete(h.teams, teamID)
	delete(h.teamMembers, teamID)
	delete(h.teamProjects, teamID)
	w.WriteHeader(http.StatusNoContent)
}

// teamMemberResponse returns a copy of a team member with its links.
func teamMemberResponse(m *TeamMember, withinCollection bool) *TeamMember {
	c := *m
	c.Links = MakeHALLinks("self=/teams/" + url.PathEscape(m.TeamID) + "/members/" + url.PathEscape(m.UserID))
	if !withinCollection {
		c.Links["ref:users:0"] = HALLink{HREF: "/ref/users?in=" + url.QueryEscape(m.UserID) + refSig}
	}
	return &c
}

func (h *Handler) handleListTeamMembers(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	teamID := chi.URLParam(req, "team_id")
	if _, ok := h.teams[teamID]; !ok {
		writeAuthError(w, http.StatusNotFound, "Requested team does not exist.")
		return
	}
	items := make([]*TeamMember, 0, len(h.teamMembers[teamID]))
	userIDs := make([]string, 0, len(h.teamMembers[teamID]))
	for _, m := range h.teamMembers[teamID] {
		items = append(items, teamMemberResponse(m, true))
		userIDs = append(userIDs, m.UserID)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Items []*TeamMember `json:"items"`
		Links HalLinks      `json:"_links,omitempty"`
	}{
		Items: items,
		Links: MakeHALLinks(
			"self=/teams/"+url.PathEscape(teamID)+"/members",
			"ref:users:0=/ref/users?in="+strings.Join(userIDs, ",")+refSig,
		),
	})
}

func (h *Handler) handleGetTeamMember(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	teamID := chi.URLParam(req, "team_id")
	userID := chi.URLParam(req, "user_id")
	i := slices.IndexFunc(h.teamMembers[teamID], func(m *TeamMember) bool { return m.UserID == userID })
	if i == -1 {
		writeAuthError(w, http.StatusNotFound, "The specified team member does not exist.")
		return
	}
	_ = json.NewEncoder(w).Encode(teamMemberResponse(h.teamMembers[teamID][i], false))
}

func (h *Handler) handleAddTeamMember(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	t, ok := h.teams[teamID]
	if !ok {
		writeAuthError(w, http.StatusNotFound, "Requested team does not exist.")
		return
	}
	var body struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.UserID == "" {
		writeAuthError(w, http.StatusBadRequest, "Invalid input.")
		return
	}
	if h.findOrgMember(t.OrganizationID, body.UserID) == -1 {
		writeAuthError(w, http.StatusBadRequest, "The user does not belong to the organization")
		return
	}
	if slices.ContainsFunc(h.teamMembers[teamID], func(m *TeamMember) bool { return m.UserID == body.UserID }) {
		writeAuthError(w, http.StatusConflict, "The specified user is already a member.")
		return
	}
	now := time.Now()
	m := &TeamMember{TeamID: teamID, UserID: body.UserID, CreatedAt: now, UpdatedAt: now}
	h.teamMembers[teamID] = append(h.teamMembers[teamID], m)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(teamMemberResponse(m, false))
}

func (h *Handler) handleDeleteTeamMember(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	userID := chi.URLParam(req, "user_id")
	i := slices.IndexFunc(h.teamMembers[teamID], func(m *TeamMember) bool { return m.UserID == userID })
	if i == -1 {
		writeAuthError(w, http.StatusNotFound, "The specified team member does not exist.")
		return
	}
	h.teamMembers[teamID] = slices.Delete(slices.Clone(h.teamMembers[teamID]), i, i+1)
	w.WriteHeader(http.StatusNoContent)
}

// handleListTeamProjects lists a team's projects. Like the API, it sorts by project title by default.
func (h *Handler) handleListTeamProjects(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	teamID := chi.URLParam(req, "team_id")
	t, ok := h.teams[teamID]
	if !ok {
		writeProblem(w, http.StatusNotFound, errors.New("team not found"))
		return
	}
	items := make([]*TeamProjectAccess, 0, len(h.teamProjects[teamID]))
	projectIDs := make([]string, 0, len(h.teamProjects[teamID]))
	for _, p := range h.teamProjects[teamID] {
		path := "/teams/" + url.PathEscape(teamID) + "/project-access/" + url.PathEscape(p.ProjectID)
		c := *p
		c.Links = MakeHALLinks("self="+path, "delete="+path)
		items = append(items, &c)
		projectIDs = append(projectIDs, p.ProjectID)
	}
	sort := req.URL.Query().Get("sort")
	switch strings.TrimPrefix(sort, "-") {
	case "", "project_title":
		slices.SortStableFunc(items, func(a, b *TeamProjectAccess) int {
			return strings.Compare(a.ProjectTitle, b.ProjectTitle)
		})
	case "granted_at":
		slices.SortStableFunc(items, func(a, b *TeamProjectAccess) int { return timeCompare(a.GrantedAt, b.GrantedAt) })
	case "updated_at":
		slices.SortStableFunc(items, func(a, b *TeamProjectAccess) int { return timeCompare(a.UpdatedAt, b.UpdatedAt) })
	default:
		writeProblem(w, http.StatusBadRequest, errors.New("invalid sort field: "+sort))
		return
	}
	if strings.HasPrefix(sort, "-") {
		slices.Reverse(items)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Items []*TeamProjectAccess `json:"items"`
		Links HalLinks             `json:"_links,omitempty"`
	}{Items: items, Links: MakeHALLinks(
		"self=/teams/"+url.PathEscape(teamID)+"/project-access",
		"ref:projects:0=/ref/projects?in="+strings.Join(projectIDs, ",")+refSig,
		"ref:organizations:0=/ref/organizations?in="+url.QueryEscape(t.OrganizationID)+refSig,
	)})
}

// handleAddTeamProjects adds projects to a team. Like the API, it adds all of them or none.
func (h *Handler) handleAddTeamProjects(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	t, ok := h.teams[teamID]
	if !ok {
		writeProblem(w, http.StatusNotFound, errors.New("team not found"))
		return
	}
	var body []struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body) == 0 {
		writeProblem(w, http.StatusBadRequest, errors.New("at least one project is required"))
		return
	}
	seen := make(map[string]bool, len(body))
	for _, item := range body {
		p, ok := h.projects[item.ProjectID]
		switch {
		case !ok:
			writeProblem(w, http.StatusBadRequest, errors.New("validation failed: projects do not exist; IDs: "+item.ProjectID))
			return
		case p.Organization != t.OrganizationID:
			writeProblem(w, http.StatusBadRequest, errors.New("validation failed: team_organization_id"))
			return
		case seen[p.ID] ||
			slices.ContainsFunc(h.teamProjects[teamID], func(a *TeamProjectAccess) bool { return a.ProjectID == p.ID }):
			writeProblem(w, http.StatusConflict, errors.New("project access already exists"))
			return
		}
		seen[p.ID] = true
	}
	now := time.Now()
	for _, item := range body {
		p := h.projects[item.ProjectID]
		h.teamProjects[teamID] = append(h.teamProjects[teamID], &TeamProjectAccess{
			TeamID:         teamID,
			OrganizationID: t.OrganizationID,
			ProjectID:      p.ID,
			ProjectTitle:   p.Title,
			GrantedAt:      now,
			UpdatedAt:      now,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteTeamProject removes a project from a team. Like the API, it succeeds if the team does not have it.
func (h *Handler) handleDeleteTeamProject(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	projectID := chi.URLParam(req, "project_id")
	i := slices.IndexFunc(h.teamProjects[teamID], func(a *TeamProjectAccess) bool { return a.ProjectID == projectID })
	if i != -1 {
		h.teamProjects[teamID] = slices.Delete(slices.Clone(h.teamProjects[teamID]), i, i+1)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListProjectTeamAccess(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	projectPath := "/projects/" + url.PathEscape(projectID)
	items := make([]*TeamProjectAccess, 0)
	for _, projects := range h.teamProjects {
		for _, p := range projects {
			if p.ProjectID == projectID {
				c := *p
				c.Links = MakeHALLinks("self=" + projectPath + "/team-access/" + url.PathEscape(p.TeamID))
				items = append(items, &c)
			}
		}
	}
	slices.SortFunc(items, func(a, b *TeamProjectAccess) int { return strings.Compare(a.TeamID, b.TeamID) })
	_ = json.NewEncoder(w).Encode(struct {
		Items []*TeamProjectAccess `json:"items"`
		Links HalLinks             `json:"_links,omitempty"`
	}{Items: items, Links: MakeHALLinks("self=" + projectPath + "/team-access")})
}
