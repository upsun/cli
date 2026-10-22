package mockapi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errorRecorder records the errors reported by the validator.
type errorRecorder struct {
	testing.TB
	mu     sync.Mutex
	errors []string
}

func (r *errorRecorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func TestValidateOpenAPI(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		route   string // Defaults to the path.
		path    string
		body    string
		status  int
		resp    string
		wantErr string
	}{
		{
			name: "valid", method: "GET", path: "/projects/p/variables/v",
			resp: `{"id": "v", "name": "v", "is_sensitive": false, "_links": {}}`,
		},
		{
			name: "invalid response", method: "GET", path: "/projects/p/variables/v",
			resp:    `{"name": 1}`,
			wantErr: "invalid response",
		},
		{
			name: "unknown response property", method: "GET", path: "/projects/p/variables/v",
			resp:    `{"name": "v", "unknown": true}`,
			wantErr: `property "unknown" is unsupported`,
		},
		{
			name: "invalid request", method: "POST", path: "/projects/p/variables",
			body:    `{"name": 1}`,
			resp:    `{"status": "OK", "code": 200}`,
			wantErr: "invalid request",
		},
		{
			name: "undocumented status", method: "POST", path: "/organizations",
			body:    `{"label": "Org"}`,
			status:  http.StatusAccepted,
			resp:    `{}`,
			wantErr: "status is not supported",
		},
		{
			name: "error responses are not checked", method: "GET", path: "/projects/p/variables/v",
			status: http.StatusNotFound, resp: `{"name": 1}`,
		},
		{
			name: "not in the spec", method: "GET", path: "/billing/profiles/bp",
			resp: `{"anything": true}`,
		},
		{
			name: "not in the spec, with an encoded slash", method: "GET",
			route: "/projects/{p}/environments/{e}/settings", path: "/projects/p/environments/feature%2Fx/settings",
			resp: `{"anything": true}`,
		},
		{
			name: "unknown route", method: "GET", path: "/unknown",
			resp:    `{}`,
			wantErr: "no matching operation",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &errorRecorder{TB: t}
			r := chi.NewRouter()
			r.Use(validateOpenAPI(rec))
			route := c.route
			if route == "" {
				route = c.path
			}
			r.MethodFunc(c.method, route, func(w http.ResponseWriter, req *http.Request) {
				_, _ = io.ReadAll(req.Body)
				if c.status != 0 {
					w.WriteHeader(c.status)
				}
				_, _ = w.Write([]byte(c.resp))
			})

			req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, c.resp, w.Body.String())
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			if c.wantErr == "" {
				assert.Empty(t, rec.errors)
			} else {
				require.Len(t, rec.errors, 1)
				assert.Contains(t, rec.errors[0], c.wantErr)
			}
		})
	}
}
