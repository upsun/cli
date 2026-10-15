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
