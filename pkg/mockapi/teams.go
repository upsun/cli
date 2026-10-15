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
	Links              HalLinks   `json:"_links"`
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
	Links     HalLinks  `json:"_links"`
}

type TeamProjectAccess struct {
	TeamID         string    `json:"team_id"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      string    `json:"project_id"`
	ProjectTitle   string    `json:"project_title"`
	GrantedAt      time.Time `json:"granted_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Links          HalLinks  `json:"_links"`
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
func (h *Handler) teamResponse(t *Team) *Team {
	c := *t
	c.Counts = TeamCounts{MemberCount: len(h.teamMembers[t.ID]), ProjectCount: len(h.teamProjects[t.ID])}
	c.Links = MakeHALLinks("self=/teams/" + url.PathEscape(t.ID))
	return &c
}

func (h *Handler) handleListTeams(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	orgID := req.URL.Query().Get("filter[organization_id]")
	var ids []string
	if in := req.URL.Query().Get("filter[id][in]"); in != "" {
		ids = strings.Split(in, ",")
	}
	items := make([]*Team, 0, len(h.teams))
	for _, t := range h.teams {
		if (orgID == "" || t.OrganizationID == orgID) && (ids == nil || slices.Contains(ids, t.ID)) {
			items = append(items, h.teamResponse(t))
		}
	}
	slices.SortFunc(items, func(a, b *Team) int { return strings.Compare(a.Label, b.Label) })
	if strings.HasPrefix(req.URL.Query().Get("sort"), "-") {
		slices.Reverse(items)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Items []*Team  `json:"items"`
		Links HalLinks `json:"_links"`
	}{Items: items, Links: MakeHALLinks("self=/teams")})
}

func (h *Handler) handleCreateTeam(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	var t Team
	if err := json.NewDecoder(req.Body).Decode(&t); err != nil || t.Label == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if _, ok := h.orgs[t.OrganizationID]; !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, existing := range h.teams {
		if existing.OrganizationID == t.OrganizationID && strings.EqualFold(existing.Label, t.Label) {
			w.WriteHeader(http.StatusConflict)
			return
		}
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
	_ = json.NewEncoder(w).Encode(h.teamResponse(&t))
}

func (h *Handler) handleGetTeam(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	t, ok := h.teams[chi.URLParam(req, "team_id")]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(h.teamResponse(t))
}

func (h *Handler) handlePatchTeam(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	t, ok := h.teams[chi.URLParam(req, "team_id")]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body struct {
		Label              *string  `json:"label"`
		ProjectPermissions []string `json:"project_permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
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
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeleteTeam(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	if _, ok := h.teams[teamID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	delete(h.teams, teamID)
	delete(h.teamMembers, teamID)
	delete(h.teamProjects, teamID)
	w.WriteHeader(http.StatusNoContent)
}

func teamMemberResponse(m *TeamMember) *TeamMember {
	c := *m
	c.Links = MakeHALLinks("self=/teams/" + url.PathEscape(m.TeamID) + "/members/" + url.PathEscape(m.UserID))
	return &c
}

func (h *Handler) handleListTeamMembers(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	teamID := chi.URLParam(req, "team_id")
	if _, ok := h.teams[teamID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	items := make([]*TeamMember, 0, len(h.teamMembers[teamID]))
	userIDs := make([]string, 0, len(h.teamMembers[teamID]))
	for _, m := range h.teamMembers[teamID] {
		items = append(items, teamMemberResponse(m))
		userIDs = append(userIDs, m.UserID)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Items []*TeamMember `json:"items"`
		Links HalLinks      `json:"_links"`
	}{
		Items: items,
		Links: MakeHALLinks(
			"self=/teams/"+url.PathEscape(teamID)+"/members",
			"ref:users:0=/ref/users?in="+strings.Join(userIDs, ","),
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
		w.WriteHeader(http.StatusNotFound)
		return
	}
	m := teamMemberResponse(h.teamMembers[teamID][i])
	m.Links["ref:users:0"] = HALLink{HREF: "/ref/users?in=" + url.QueryEscape(userID)}
	_ = json.NewEncoder(w).Encode(m)
}

func (h *Handler) handleAddTeamMember(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	if _, ok := h.teams[teamID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.UserID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if slices.ContainsFunc(h.teamMembers[teamID], func(m *TeamMember) bool { return m.UserID == body.UserID }) {
		w.WriteHeader(http.StatusConflict)
		return
	}
	now := time.Now()
	m := &TeamMember{TeamID: teamID, UserID: body.UserID, CreatedAt: now, UpdatedAt: now}
	h.teamMembers[teamID] = append(h.teamMembers[teamID], m)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(teamMemberResponse(m))
}

func (h *Handler) handleDeleteTeamMember(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	userID := chi.URLParam(req, "user_id")
	i := slices.IndexFunc(h.teamMembers[teamID], func(m *TeamMember) bool { return m.UserID == userID })
	if i == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.teamMembers[teamID] = slices.Delete(slices.Clone(h.teamMembers[teamID]), i, i+1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListTeamProjects(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	teamID := chi.URLParam(req, "team_id")
	if _, ok := h.teams[teamID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	items := make([]*TeamProjectAccess, 0, len(h.teamProjects[teamID]))
	for _, p := range h.teamProjects[teamID] {
		c := *p
		c.Links = MakeHALLinks("self=/teams/" + url.PathEscape(teamID) + "/project-access/" + url.PathEscape(p.ProjectID))
		items = append(items, &c)
	}
	slices.SortFunc(items, func(a, b *TeamProjectAccess) int { return strings.Compare(a.ProjectTitle, b.ProjectTitle) })
	_ = json.NewEncoder(w).Encode(struct {
		Items []*TeamProjectAccess `json:"items"`
		Links HalLinks             `json:"_links"`
	}{Items: items, Links: MakeHALLinks("self=/teams/" + url.PathEscape(teamID) + "/project-access")})
}

func (h *Handler) handleAddTeamProjects(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	t, ok := h.teams[teamID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body []struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, item := range body {
		if p, ok := h.projects[item.ProjectID]; !ok || p.Organization != t.OrganizationID {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	now := time.Now()
	for _, item := range body {
		p := h.projects[item.ProjectID]
		if slices.ContainsFunc(h.teamProjects[teamID], func(a *TeamProjectAccess) bool { return a.ProjectID == p.ID }) {
			continue
		}
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

func (h *Handler) handleDeleteTeamProject(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	teamID := chi.URLParam(req, "team_id")
	projectID := chi.URLParam(req, "project_id")
	i := slices.IndexFunc(h.teamProjects[teamID], func(a *TeamProjectAccess) bool { return a.ProjectID == projectID })
	if i == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.teamProjects[teamID] = slices.Delete(slices.Clone(h.teamProjects[teamID]), i, i+1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListProjectTeamAccess(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	projectID := chi.URLParam(req, "project_id")
	type teamAccess struct {
		TeamID    string    `json:"team_id"`
		ProjectID string    `json:"project_id"`
		GrantedAt time.Time `json:"granted_at"`
	}
	items := make([]teamAccess, 0)
	for teamID, projects := range h.teamProjects {
		for _, p := range projects {
			if p.ProjectID == projectID {
				items = append(items, teamAccess{TeamID: teamID, ProjectID: projectID, GrantedAt: p.GrantedAt})
			}
		}
	}
	slices.SortFunc(items, func(a, b teamAccess) int { return strings.Compare(a.TeamID, b.TeamID) })
	_ = json.NewEncoder(w).Encode(struct {
		Items []teamAccess `json:"items"`
		Links HalLinks     `json:"_links"`
	}{Items: items, Links: MakeHALLinks("self=/projects/" + url.PathEscape(projectID) + "/team-access")})
}
