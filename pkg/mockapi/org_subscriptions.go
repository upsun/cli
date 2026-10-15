package mockapi

import (
	"cmp"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// handleListOrgSubscriptions lists the subscriptions of an organization's
// projects, paginated with the "page[size]" and "page[after]" query parameters.
// Subscriptions created through the mock are listed as stored.
func (h *Handler) handleListOrgSubscriptions(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	orgID := chi.URLParam(req, "organization_id")
	collectionPath := "/organizations/" + url.PathEscape(orgID) + "/subscriptions"
	subs := make([]*Subscription, 0)
	for _, p := range h.projects {
		if p.Organization != orgID || p.SubscriptionID == "" {
			continue
		}
		if sub, ok := h.subscriptions[p.SubscriptionID]; ok {
			c := *sub
			subs = append(subs, &c)
			continue
		}
		subs = append(subs, &Subscription{
			ID:            p.SubscriptionID,
			Links:         MakeHALLinks("self=" + collectionPath + "/" + url.PathEscape(p.SubscriptionID)),
			ProjectID:     p.ID,
			ProjectRegion: p.Region,
			ProjectTitle:  p.Title,
			Status:        "active",
		})
	}
	slices.SortFunc(subs, func(a, b *Subscription) int { return cmp.Compare(a.ID, b.ID) })

	query := req.URL.Query()
	if after := query.Get("page[after]"); after != "" {
		subs = slices.DeleteFunc(subs, func(s *Subscription) bool { return s.ID <= after })
	}
	links := MakeHALLinks("self=" + req.URL.String())
	if size, err := strconv.Atoi(query.Get("page[size]")); err == nil && size > 0 && len(subs) > size {
		subs = subs[:size]
		query.Set("page[after]", subs[size-1].ID)
		links["next"] = HALLink{HREF: collectionPath + "?" + query.Encode()}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": subs, "_links": links})
}

func (h *Handler) handleDeleteOrg(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	orgID := chi.URLParam(req, "organization_id")
	if _, ok := h.orgs[orgID]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// Like the API, refuse to delete an organization that owns projects.
	for _, p := range h.projects {
		if p.Organization == orgID {
			w.WriteHeader(http.StatusConflict)
			return
		}
	}
	delete(h.orgs, orgID)
	w.WriteHeader(http.StatusNoContent)
}

// OrgIDs returns the IDs of the organizations.
func (s *store) OrgIDs() []string {
	s.RLock()
	defer s.RUnlock()
	ids := make([]string, 0, len(s.orgs))
	for id := range s.orgs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
