package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// Resource is a generic API resource.
type Resource interface {
	GetLink(name string) (string, bool)
}

// HALLinks represents the HAL links on a resource.
// Most are an object containing a single "href", but some are arrays e.g. "curies".
type HALLinks map[string]any

// maxErrorBody is the length of an error response's body that is kept for the error message.
const maxErrorBody = 1024

// getResource fetches an API resource from a URL and decodes it into an interface.
func (c *Client) getResource(ctx context.Context, urlStr string, r any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, http.NoBody)
	if err != nil {
		return Error{Original: err, URL: urlStr}
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Error{Original: err, URL: urlStr, Response: resp}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return Error{Response: resp, URL: urlStr, Body: body}
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return Error{Original: err, Response: resp, URL: urlStr}
	}
	return nil
}
