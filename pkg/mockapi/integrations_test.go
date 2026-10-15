package mockapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteMissingIntegration(t *testing.T) {
	h := NewHandler(t)
	server := httptest.NewServer(h)
	defer server.Close()

	// Twice, to check that the mock is still responsive.
	for range 2 {
		req, err := http.NewRequest(http.MethodDelete, server.URL+"/projects/p/integrations/missing", http.NoBody)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer test")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	}
}

func TestDeleteIntegrationKeepsCallerSlice(t *testing.T) {
	h := NewHandler(t)
	path := "/projects/p/integrations/"
	integrations := []*Integration{
		{ID: "a", Links: MakeHALLinks("self=" + path + "a")},
		{ID: "b", Links: MakeHALLinks("self=" + path + "b")},
	}
	h.SetProjectIntegrations("p", integrations)
	server := httptest.NewServer(h)
	defer server.Close()

	req, err := http.NewRequest(http.MethodDelete, server.URL+path+"a", http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Len(t, h.Integrations("p"), 1)
	assert.Equal(t, "a", integrations[0].ID)
	assert.Equal(t, "b", integrations[1].ID)
}
