package mockapi

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Subscription list page sizes, as in the API.
const (
	defaultSubscriptionPageSize = 50
	maxSubscriptionPageSize     = 100
)

// handleListOrgSubscriptions lists the subscriptions of an organization's
// projects. Like the API, it filters by status (excluding deleted subscriptions
// by default), and paginates with "page[size]" and an opaque "page[after]"
// cursor. Subscriptions created through the mock are listed as stored.
func (h *Handler) handleListOrgSubscriptions(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	size := defaultSubscriptionPageSize
	if s := query.Get("page[size]"); s != "" {
		n, _ := strconv.Atoi(s)
		if n < 1 {
			writeProblem(w, http.StatusBadRequest, errors.New("invalid page size"))
			return
		}
		if n > maxSubscriptionPageSize {
			writeProblem(w, http.StatusBadRequest, errors.New("page size must not be larger than 100"))
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
	statuses := statusFilter(query)

	h.RLock()
	defer h.RUnlock()
	orgID := chi.URLParam(req, "organization_id")
	collectionPath := "/organizations/" + url.PathEscape(orgID) + "/subscriptions"
	subs := make([]*Subscription, 0)
	for _, p := range h.projects {
		if p.Organization != orgID || p.SubscriptionID == "" {
			continue
		}
		sub := &Subscription{
			ID:            p.SubscriptionID,
			Links:         MakeHALLinks("self=" + collectionPath + "/" + url.PathEscape(p.SubscriptionID)),
			ProjectID:     p.ID,
			ProjectRegion: p.Region,
			ProjectTitle:  p.Title,
			Status:        "active",
		}
		if stored, ok := h.subscriptions[p.SubscriptionID]; ok {
			c := *stored
			sub = &c
		}
		if (statuses == nil && sub.Status != "deleted") || slices.Contains(statuses, sub.Status) {
			subs = append(subs, sub)
		}
	}
	count := len(subs)
	slices.SortFunc(subs, func(a, b *Subscription) int { return cmp.Compare(a.ID, b.ID) })
	if after != "" {
		subs = slices.DeleteFunc(subs, func(s *Subscription) bool { return s.ID <= after })
	}

	links := MakeHALLinks("self=" + collectionPath)
	if len(subs) > size {
		subs = subs[:size]
		query.Set("page[after]", encodeCursor(subs[size-1].ID))
		links["next"] = HALLink{HREF: collectionPath + "?" + query.Encode()}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"count": count, "items": subs, "_links": links})
}

// statusFilterKey matches the forms of the "filter[status]" query parameter:
// with or without "[value]", and with an optional "[]" or indexed "[0]" suffix.
var statusFilterKey = regexp.MustCompile(`^filter\[status\](\[value\])?(\[\d*\])?$`)

// statusFilter returns the statuses in a "filter[status]" query parameter, or nil if there is no filter.
func statusFilter(query url.Values) []string {
	var statuses []string
	for key, values := range query {
		if statusFilterKey.MatchString(key) {
			statuses = append(statuses, values...)
		}
	}
	return statuses
}

// encodeCursor encodes a cursor for an ID, as an opaque string like the API's.
func encodeCursor(id string) string {
	b, _ := json.Marshal([]string{id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(cursor string) (id string, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", false
	}
	var values []string
	if json.Unmarshal(b, &values) != nil || len(values) != 1 {
		return "", false
	}
	return values[0], true
}

// errOrgOwnsProjects is the API's error (from the accounts service) when deleting an organization that owns projects.
var errOrgOwnsProjects = errors.New( //nolint:staticcheck // The API's message.
	"The organization owns projects so it cannot be deleted. " +
		"Delete the projects first, or transfer them to another organization.")

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
			writeProblem(w, http.StatusBadRequest, errOrgOwnsProjects)
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
