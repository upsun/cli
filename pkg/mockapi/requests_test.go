package mockapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequests(t *testing.T) {
	h := NewHandler(t)
	h.SetProjects([]*Project{{ID: "p", DefaultBranch: "main"}})
	h.RejectAccessToken("rejected")
	server := httptest.NewServer(h)
	defer server.Close()

	send := func(method, path, token, body string) {
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	send("GET", "/projects/p?foo=bar", "token", "")
	send("POST", "/projects/p/integrations", "token", `{"type":"webhook","url":"https://example.com"}`)
	send("POST", "/projects/p/integrations", "rejected", `{"type":"script"}`)

	assert.Len(t, h.AllRequests(), 3)

	gets := h.Requests("GET", "/projects/*")
	require.Len(t, gets, 1)
	assert.Equal(t, "bar", gets[0].Query.Get("foo"))
	assert.Empty(t, gets[0].Body)

	posts := h.Requests("POST", "/projects/*/integrations")
	require.Len(t, posts, 2)
	assert.JSONEq(t, `{"type":"webhook","url":"https://example.com"}`, posts[0].Body)
	assert.Equal(t, "Bearer rejected", posts[1].Header.Get("Authorization"))

	assert.Empty(t, h.Requests("PATCH", "/projects/*/integrations"))
	assert.Empty(t, h.Requests("POST", "/projects/*"))
}
