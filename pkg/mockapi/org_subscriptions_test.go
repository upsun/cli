package mockapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgSubscriptions(t *testing.T) {
	h := NewHandler(t)
	h.SetOrgs([]*Org{{ID: "org1"}, {ID: "org2"}})
	h.SetProjects([]*Project{
		{ID: "p1", Title: "P1", Organization: "org1", SubscriptionID: "1001"},
		{ID: "p2", Title: "P2", Organization: "org1", SubscriptionID: "1002"},
	})
	// A stored subscription, as if it was created through the mock, and still provisioning.
	h.subscriptions = map[string]*Subscription{"1002": {
		ID:     "1002",
		Status: "provisioning",
		Links:  MakeHALLinks("self=/organizations/org1/subscriptions/1002"),
	}}
	server := httptest.NewServer(h)
	defer server.Close()

	// do sends a request, decodes the response into out if it is not nil, and returns the status code.
	do := func(method, path, body string, out any) int {
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer test")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		if out != nil {
			require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
		}
		return resp.StatusCode
	}

	// The stored subscription is listed as stored.
	var list struct {
		Items []*Subscription `json:"items"`
	}
	require.Equal(t, http.StatusOK, do(http.MethodGet, "/organizations/org1/subscriptions", "", &list))
	statuses := make(map[string]string, len(list.Items))
	for _, s := range list.Items {
		statuses[s.ID] = s.Status
	}
	assert.Equal(t, map[string]string{"1001": "active", "1002": "provisioning"}, statuses)

	// The listed subscriptions can be read.
	for _, item := range list.Items {
		var sub Subscription
		require.Equal(t, http.StatusOK, do(http.MethodGet, item.Links["self"].HREF, "", &sub))
		assert.Equal(t, item.ID, sub.ID)
		require.Equal(t, http.StatusOK, do(http.MethodGet, "/subscriptions/"+item.ID, "", &sub))
		assert.Equal(t, item.Status, sub.Status)
	}
	assert.Equal(t, http.StatusNotFound, do(http.MethodGet, "/organizations/org2/subscriptions/1001", "", nil))

	// Status filters, including the indexed form that PHP's http_build_query() produces.
	for _, query := range []string{
		"filter[status][value][]=active&filter[status][operator]=IN",
		"filter[status][value][0]=active&filter[status][value][1]=suspended&filter[status][operator]=IN",
		"filter[status]=active",
	} {
		require.Equal(t, http.StatusOK, do(http.MethodGet, "/organizations/org1/subscriptions?"+query, "", &list), query)
		require.Len(t, list.Items, 1, query)
		assert.Equal(t, "1001", list.Items[0].ID, query)
	}

	// Pages, with an opaque cursor.
	assert.Equal(t, http.StatusBadRequest, do(http.MethodGet, "/organizations/org1/subscriptions?page[size]=101", "", nil))
	var page struct {
		Count int                `json:"count"`
		Items []*Subscription    `json:"items"`
		Links map[string]HALLink `json:"_links"`
	}
	require.Equal(t, http.StatusOK, do(http.MethodGet, "/organizations/org1/subscriptions?page[size]=1", "", &page))
	assert.Equal(t, 2, page.Count)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "/organizations/org1/subscriptions", page.Links["self"].HREF)
	next := page.Links["next"].HREF
	require.NotEmpty(t, next)
	assert.NotContains(t, next, page.Items[0].ID)
	first := page.Items[0].ID
	page.Links = nil
	require.Equal(t, http.StatusOK, do(http.MethodGet, next, "", &page))
	require.Len(t, page.Items, 1)
	assert.NotEqual(t, first, page.Items[0].ID)
	assert.NotContains(t, page.Links, "next")

	// An organization that owns projects cannot be deleted.
	assert.Equal(t, http.StatusBadRequest, do(http.MethodDelete, "/organizations/org1", "", nil))
	assert.Equal(t, http.StatusNoContent, do(http.MethodDelete, "/organizations/org2", "", nil))
	assert.Equal(t, []string{"org1"}, h.OrgIDs())
}
