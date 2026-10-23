package mockapi

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
)

// Request is a request received by the mock API.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   string
}

// recordRequests records every request, including those that fail.
func (h *Handler) recordRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			h.t.Errorf("failed to read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		h.Lock()
		h.requests = append(h.requests, Request{
			Method: req.Method,
			Path:   req.URL.Path,
			Query:  req.URL.Query(),
			Header: req.Header.Clone(),
			Body:   string(body),
		})
		h.Unlock()
		next.ServeHTTP(w, req)
	})
}

// AllRequests returns every request received, in order.
func (h *Handler) AllRequests() []Request {
	h.RLock()
	defer h.RUnlock()
	return slices.Clone(h.requests)
}

// Requests returns the requests received with a method and a path matching a pattern, in order.
// The pattern is for path.Match, where "*" matches one path segment.
func (h *Handler) Requests(method, pattern string) []Request {
	if _, err := path.Match(pattern, ""); err != nil {
		h.t.Errorf("invalid request path pattern %q: %v", pattern, err)
		return nil
	}
	return slices.DeleteFunc(h.AllRequests(), func(r Request) bool {
		ok, _ := path.Match(pattern, r.Path)
		return r.Method != method || !ok
	})
}
