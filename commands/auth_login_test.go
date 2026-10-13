package commands

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoginServer(t *testing.T) {
	cases := []struct {
		name       string
		host       string
		query      string
		wantStatus int
		wantResult bool
	}{
		{name: "start", query: "", wantStatus: http.StatusFound},
		{name: "other host", host: "attacker.example:1234", query: "", wantStatus: http.StatusMisdirectedRequest},
		{name: "code", query: "state=s&code=c", wantStatus: http.StatusFound, wantResult: true},
		{name: "code with wrong state", query: "state=x&code=c", wantStatus: http.StatusUnauthorized},
		{name: "error", query: "state=s&error=access_denied", wantStatus: http.StatusUnauthorized, wantResult: true},
		{name: "error without state", query: "error=access_denied", wantStatus: http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ls := &loginServer{
				cnf:       testConfig(),
				localURL:  "http://127.0.0.1:5555",
				authorize: "https://auth.example.com/oauth2/authorize",
				state:     "s",
				result:    make(chan loginResult, 1),
			}
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:5555/?"+c.query, http.NoBody)
			if c.host != "" {
				req.Host = c.host
			}
			w := httptest.NewRecorder()
			ls.ServeHTTP(w, req)
			assert.Equal(t, c.wantStatus, w.Code)
			assert.Equal(t, c.wantResult, len(ls.result) == 1)
		})
	}
}
