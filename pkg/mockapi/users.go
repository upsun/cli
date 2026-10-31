package mockapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func (h *Handler) handleUsersMe(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(h.myUser)
}

func (h *Handler) handleUserRefs(w http.ResponseWriter, req *http.Request) {
	require.NoError(h.t, req.ParseForm())
	ids := strings.Split(req.Form.Get("in"), ",")
	userRefs := make(map[string]UserRef, len(ids))
	for _, id := range ids {
		userRefs[id] = UserRef{ID: id, Email: id + "@example.com", Username: id, FirstName: "User", LastName: id}
	}
	_ = json.NewEncoder(w).Encode(userRefs)
}

// handleGetUser gets the current user, or another user like those of user references, by ID or by "email=...".
func (h *Handler) handleGetUser(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	id := chi.URLParam(req, "user_id")
	if email, ok := strings.CutPrefix(id, "email%3D"); ok {
		email, _ = url.PathUnescape(email)
		switch {
		case h.myUser != nil && email == h.myUser.Email:
			id = h.myUser.ID
		case strings.HasSuffix(email, "@example.com"):
			id = strings.TrimSuffix(email, "@example.com")
		default:
			writeAuthError(w, http.StatusNotFound, "User not found.")
			return
		}
	}
	if h.myUser != nil && id == h.myUser.ID {
		_ = json.NewEncoder(w).Encode(h.myUser)
		return
	}
	_ = json.NewEncoder(w).Encode(&User{ID: id, Email: id + "@example.com", Username: id, FirstName: "User", LastName: id})
}

// handleUserExtendedAccess lists a user's project access, optionally filtered by organization.
func (h *Handler) handleUserExtendedAccess(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	userID := chi.URLParam(req, "user_id")
	require.NoError(h.t, req.ParseForm())
	require.Equal(h.t, "project", req.Form.Get("filter[resource_type]"))
	orgID := req.Form.Get("filter[organization_id]")
	var (
		projectGrants = make([]*UserGrant, 0, len(h.userGrants))
		projectIDs    = make(uniqueMap)
		orgIDs        = make(uniqueMap)
	)
	for _, g := range h.userGrants {
		if g.ResourceType == "project" && g.UserID == userID && (orgID == "" || g.OrganizationID == orgID) {
			projectGrants = append(projectGrants, g)
			projectIDs[g.ResourceID] = struct{}{}
			orgIDs[g.OrganizationID] = struct{}{}
		}
	}
	ret := struct {
		Items []*UserGrant `json:"items"`
		Links HalLinks     `json:"_links,omitempty"`
	}{Items: projectGrants, Links: MakeHALLinks(
		"ref:projects:0=/ref/projects?in="+strings.Join(projectIDs.keys(), ",")+refSig,
		"ref:organizations:0=/ref/organizations?in="+strings.Join(orgIDs.keys(), ",")+refSig,
	)}
	_ = json.NewEncoder(w).Encode(ret)
}

type uniqueMap map[string]struct{}

func (m uniqueMap) keys() []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	return r
}
