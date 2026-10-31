package mockapi

import (
	"encoding/json"
	"net/http"
	"path"
	"slices"
)

// fault is an error response to send instead of handling a request.
type fault struct {
	method, pattern string
	status          int
	body            string
}

// FailNext makes the next request with a method and a path matching a pattern fail with a status and a body, instead
// of reaching its route. The pattern is for path.Match, as in Requests. Each call fails one request.
//
// A JSON body is sent as an API error (see Problem), and any other body as HTML, like a proxy's error page.
func (h *Handler) FailNext(method, pattern string, status int, body string) {
	if _, err := path.Match(pattern, ""); err != nil {
		h.t.Errorf("invalid request path pattern %q: %v", pattern, err)
		return
	}
	h.Lock()
	defer h.Unlock()
	h.faults = append(h.faults, fault{method: method, pattern: pattern, status: status, body: body})
}

// Problem returns the body of an API error (RFC 9457), with a detail message.
func Problem(status int, detail string) string {
	b, _ := json.Marshal(map[string]any{"status": status, "title": http.StatusText(status), "detail": detail})
	return string(b)
}

// injectFaults sends the first fault matching a request, if any, and removes it.
func (h *Handler) injectFaults(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h.Lock()
		i := slices.IndexFunc(h.faults, func(f fault) bool {
			ok, _ := path.Match(f.pattern, req.URL.Path)
			return ok && f.method == req.Method
		})
		var f fault
		if i >= 0 {
			f = h.faults[i]
			h.faults = slices.Delete(h.faults, i, i+1)
		}
		h.Unlock()
		if i < 0 {
			next.ServeHTTP(w, req)
			return
		}
		if json.Valid([]byte(f.body)) {
			w.Header().Set("Content-Type", "application/problem+json")
		} else {
			w.Header().Set("Content-Type", "text/html")
		}
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
	})
}
