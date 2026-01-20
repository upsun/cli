package api

import (
	"context"
)

// GetMyUserID returns the current user's ID.
func (c *Client) GetMyUserID(ctx context.Context) (string, error) {
	meURL, err := c.resolveURL("users/me")
	if err != nil {
		return "", err
	}

	var me struct {
		ID string `json:"id"`
	}
	if err := c.getResource(ctx, meURL.String(), &me); err != nil {
		return "", err
	}

	return me.ID, nil
}
