package mockapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func (h *Handler) handleOrgRefs(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	require.NoError(h.t, req.ParseForm())
	ids := strings.Split(req.Form.Get("in"), ",")
	refs := make(map[string]*OrgRef, len(ids))
	for _, id := range ids {
		if o, ok := h.orgs[id]; ok {
			refs[id] = o.AsRef()
		} else {
			refs[id] = nil
		}
	}
	_ = json.NewEncoder(w).Encode(refs)
}

func (h *Handler) handleListOrgs(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	var (
		orgs     = make([]*Org, 0, len(h.orgs))
		ownerIDs = make(uniqueMap)
		ownerID  = req.URL.Query().Get("filter[owner_id][eq]")
	)
	for _, o := range h.orgs {
		if ownerID != "" && o.Owner != ownerID {
			continue
		}
		orgs = append(orgs, o)
		ownerIDs[o.Owner] = struct{}{}
	}
	slices.SortFunc(orgs, func(a, b *Org) int { return strings.Compare(a.Name, b.Name) })
	_ = json.NewEncoder(w).Encode(struct {
		Items []*Org   `json:"items"`
		Links HalLinks `json:"_links,omitempty"`
	}{
		Items: orgs,
		Links: MakeHALLinks("ref:users:0=/ref/users?in=" + strings.Join(ownerIDs.keys(), ",") + refSig),
	})
}

func (h *Handler) handleGetOrg(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	var org *Org

	orgID := chi.URLParam(req, "organization_id")
	if strings.HasPrefix(orgID, "name%3D") {
		name := strings.TrimPrefix(orgID, "name%3D")
		for _, o := range h.orgs {
			if o.Name == name {
				org = o
				break
			}
		}
	} else {
		org = h.orgs[path.Base(req.URL.Path)]
	}

	if org == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	_ = json.NewEncoder(w).Encode(org)
}

func (h *Handler) handleCreateOrg(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	var org Org
	err := json.NewDecoder(req.Body).Decode(&org)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, o := range h.orgs {
		if o.Name == org.Name {
			w.WriteHeader(http.StatusConflict)
			return
		}
	}
	org.ID = ulid.MustNew(ulid.Now(), rand.Reader).String()
	org.Owner = h.myUser.ID
	org.Capabilities = []string{}
	org.Links = MakeHALLinks("self=/organizations/" + url.PathEscape(org.ID))
	h.orgs[org.ID] = &org
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(&org)
}

func (h *Handler) handlePatchOrg(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	orgID := chi.URLParam(req, "organization_id")
	p, ok := h.orgs[orgID]
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
	h.orgs[orgID] = &patched
	_ = json.NewEncoder(w).Encode(&patched)
}

// OrgProject is a project in an organization's project list.
type OrgProject struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Title          string    `json:"title"`
	Region         string    `json:"region"`
	Vendor         string    `json:"vendor"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Links          HalLinks  `json:"_links"`
}

// handleListOrgProjects lists an organization's projects by ID. They all have the same status, orgProjectStatus.
func (h *Handler) handleListOrgProjects(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	orgID := chi.URLParam(req, "organization_id")
	if _, ok := h.orgs[orgID]; !ok {
		writeProblem(w, http.StatusNotFound, errors.New("organization not found"))
		return
	}
	items := make([]*OrgProject, 0)
	in := req.URL.Query().Get("filter[status][in]")
	if in == "" || slices.Contains(strings.Split(in, ","), orgProjectStatus) {
		for _, p := range h.projects {
			if p.Organization != orgID {
				continue
			}
			items = append(items, &OrgProject{
				ID:             p.ID,
				OrganizationID: orgID,
				Title:          p.Title,
				Region:         p.Region,
				Vendor:         p.Vendor,
				Status:         orgProjectStatus,
				CreatedAt:      p.CreatedAt,
				UpdatedAt:      p.UpdatedAt,
				Links:          MakeHALLinks("self=/organizations/" + url.PathEscape(orgID) + "/projects/" + url.PathEscape(p.ID)),
			})
		}
	}
	slices.SortFunc(items, func(a, b *OrgProject) int { return strings.Compare(a.ID, b.ID) })
	links := MakeHALLinks("self=/organizations/" + url.PathEscape(orgID) + "/projects")
	page, ok := paginate(w, req, items, maxOrgProjectsPageSize, links)
	if !ok {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"count": len(items), "items": page, "_links": links})
}

const (
	orgProjectStatus = "active"
	// maxOrgProjectsPageSize is the maximum page size of an organization's project list, as in the spec.
	maxOrgProjectsPageSize = 100
)
