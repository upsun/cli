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
	h.SetProjects([]*Project{{ID: "p1", Title: "P1", Organization: "org1", SubscriptionID: "1001"}})
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

	// A subscription created through the mock is listed as stored, while it is provisioning.
	var created Subscription
	require.Equal(t, http.StatusOK, do(http.MethodPost, "/organizations/org1/subscriptions",
		`{"project_region":"r","project_title":"New"}`, &created))

	var list struct {
		Items []*Subscription `json:"items"`
	}
	require.Equal(t, http.StatusOK, do(http.MethodGet, "/organizations/org1/subscriptions", "", &list))
	statuses := make(map[string]string, len(list.Items))
	for _, s := range list.Items {
		statuses[s.ID] = s.Status
	}
	assert.Equal(t, map[string]string{"1001": "active", created.ID: "provisioning"}, statuses)

	// An organization that owns projects cannot be deleted.
	assert.Equal(t, http.StatusConflict, do(http.MethodDelete, "/organizations/org1", "", nil))
	assert.Equal(t, http.StatusNoContent, do(http.MethodDelete, "/organizations/org2", "", nil))
	assert.Equal(t, []string{"org1"}, h.OrgIDs())
}
