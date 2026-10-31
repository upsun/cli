package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testError struct{}

func (testError) Error() string { return "test" }

func TestError_Unwrap(t *testing.T) {
	err := error(Error{Original: testError{}, URL: "https://api.example.com"})
	var te testError
	assert.True(t, errors.As(err, &te), "the original error must be in the chain")
	assert.Nil(t, errors.Unwrap(Error{URL: "https://api.example.com"}))
}

func TestError_Message(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Internal", "secret")
		switch req.URL.Path {
		case "/organizations/forbidden":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"title":"Forbidden","detail":"No access."}` + "\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c, err := NewClient(server.URL, server.Client())
	require.NoError(t, err)

	cases := []struct {
		id   string
		want string
	}{
		{"forbidden", "API error: 403 Forbidden: " + server.URL + "/organizations/forbidden\n" +
			`{"title":"Forbidden","detail":"No access."}`},
		{"missing", "API error: 404 Not Found: " + server.URL + "/organizations/missing"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			_, err := c.GetOrganization(t.Context(), tc.id)
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
		})
	}
}
