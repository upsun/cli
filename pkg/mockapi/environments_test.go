package mockapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPatchEnvironmentHTTPAccess(t *testing.T) {
	h := NewHandler(t)
	env := &Environment{ID: "main", Project: "p", HTTPAccess: map[string]any{
		"is_enabled": true, "addresses": []any{}, "basic_auth": map[string]any{"user": "password"},
	}}
	h.SetEnvironments([]*Environment{env})

	patch := func(body string) int {
		req := httptest.NewRequest(http.MethodPatch, "/projects/p/environments/main", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusBadRequest, patch(`{"http_access": null}`))
	assert.Equal(t, map[string]any{"user": "password"}, h.environments["main"].HTTPAccess["basic_auth"])

	assert.Equal(t, http.StatusOK, patch(`{"http_access": {"basic_auth": null}}`))
	assert.Equal(t, map[string]any{}, h.environments["main"].HTTPAccess["basic_auth"])
	assert.Equal(t, true, h.environments["main"].HTTPAccess["is_enabled"])
}
