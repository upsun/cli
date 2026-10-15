package mockapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSensitiveVariablesAreRedacted(t *testing.T) {
	h := NewHandler(t)
	h.SetProjectVariables("p", []*Variable{
		{Name: "secret", Value: "s3cr3t", IsSensitive: true},
		{Name: "plain", Value: "visible"},
	})
	h.SetEnvLevelVariables("p", "main", []*EnvLevelVariable{
		{Variable: Variable{Name: "env:secret", Value: "s3cr3t", IsSensitive: true}},
		{Variable: Variable{Name: "env:plain", Value: "visible"}},
	})
	server := httptest.NewServer(h)
	defer server.Close()

	get := func(path string) any {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, http.NoBody)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer test")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var data any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&data))
		return data
	}

	cases := []struct {
		path      string
		wantValue bool
	}{
		{"/projects/p/variables/secret", false},
		{"/projects/p/variables/plain", true},
		{"/projects/p/environments/main/variables/env:secret", false},
		{"/projects/p/environments/main/variables/env:plain", true},
	}
	for _, c := range cases {
		v, ok := get(c.path).(map[string]any)
		require.True(t, ok, c.path)
		_, hasValue := v["value"]
		assert.Equal(t, c.wantValue, hasValue, c.path)
	}

	for _, path := range []string{"/projects/p/variables", "/projects/p/environments/main/variables"} {
		list, ok := get(path).([]any)
		require.True(t, ok, path)
		require.Len(t, list, 2, path)
		for _, item := range list {
			v, ok := item.(map[string]any)
			require.True(t, ok, path)
			_, hasValue := v["value"]
			assert.Equal(t, v["is_sensitive"] != true, hasValue, "%s: %v", path, v["name"])
		}
	}

	// The stored value is unchanged.
	assert.Equal(t, "s3cr3t", h.projectVariables["p"][0].Value)
}
