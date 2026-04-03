package api

import (
	"context"
)

// GetMyUser fetches the current user's account information from GET /users/me.
func (c *Client) GetMyUser(ctx context.Context, _ bool) (map[string]interface{}, error) {
	u, err := c.resolveURL("users/me")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := c.getResource(ctx, u.String(), &result); err != nil {
		return nil, err
	}
	return result, nil
}
