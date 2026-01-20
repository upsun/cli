package api

import (
	"context"
	"strings"
)

type projectRefsResponse map[string]*ProjectInfo

type orgRefsResponse map[string]*OrganizationRef

// extractHALLink finds a HAL link by prefix (e.g., "ref:projects" matches "ref:projects:0")
func extractHALLink(links HALLinks, prefix string) string {
	for key, val := range links {
		if strings.HasPrefix(key, prefix) {
			if m, ok := val.(map[string]any); ok {
				if href, ok := m["href"].(string); ok {
					return href
				}
			}
		}
	}
	return ""
}

// getProjectRefsFromLink fetches project references using a HAL link URL.
func (c *Client) getProjectRefsFromLink(ctx context.Context, linkPath string) (map[string]*ProjectInfo, error) {
	if linkPath == "" {
		return map[string]*ProjectInfo{}, nil
	}

	refURL, err := c.resolveURL(linkPath)
	if err != nil {
		return nil, err
	}

	var refs projectRefsResponse
	if err := c.getResource(ctx, refURL.String(), &refs); err != nil {
		return nil, err
	}

	return refs, nil
}

// getOrgRefsFromLink fetches organization references using a HAL link URL.
func (c *Client) getOrgRefsFromLink(ctx context.Context, linkPath string) (map[string]*OrganizationRef, error) {
	if linkPath == "" {
		return map[string]*OrganizationRef{}, nil
	}

	refURL, err := c.resolveURL(linkPath)
	if err != nil {
		return nil, err
	}

	var refs orgRefsResponse
	if err := c.getResource(ctx, refURL.String(), &refs); err != nil {
		return nil, err
	}

	return refs, nil
}
