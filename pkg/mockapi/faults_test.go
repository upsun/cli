package mockapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailNext(t *testing.T) {
	h := NewHandler(t)
	h.SetProjects([]*Project{{ID: "p", DefaultBranch: "main"}})
	h.FailNext("GET", "/projects/*", http.StatusForbidden, Problem(http.StatusForbidden, "No access"))
	h.FailNext("GET", "/projects/*", http.StatusBadGateway, "<html>Bad gateway</html>")
	server := httptest.NewServer(h)
	defer server.Close()

	get := func(path string) (int, string, string) {
		req, err := http.NewRequest("GET", server.URL+path, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer token")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
	}

	status, _, _ := get("/users/me")
	assert.Equal(t, http.StatusOK, status, "other paths succeed")

	status, contentType, body := get("/projects/p")
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, "application/problem+json", contentType)
	assert.JSONEq(t, `{"status":403,"title":"Forbidden","detail":"No access"}`, body)

	status, contentType, body = get("/projects/p")
	assert.Equal(t, http.StatusBadGateway, status)
	assert.Equal(t, "text/html", contentType)
	assert.Equal(t, "<html>Bad gateway</html>", body)

	status, _, _ = get("/projects/p")
	assert.Equal(t, http.StatusOK, status, "each fault fails one request")
	assert.Len(t, h.Requests("GET", "/projects/p"), 3)
}
