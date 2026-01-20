package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// ProjectInfo contains basic information about a project.
type ProjectInfo struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Region         string    `json:"region"`
	Status         string    `json:"status"`
	OrganizationID string    `json:"organization_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	// OrganizationRef contains reference information for the organization
	OrganizationRef *OrganizationRef `json:"-"`
}

// OrganizationRef is a reference to an organization.
type OrganizationRef struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Label   string `json:"label"`
	OwnerID string `json:"owner_id"`
}

// userGrant represents a user's access grant to a resource.
type userGrant struct {
	ResourceID     string    `json:"resource_id"`
	ResourceType   string    `json:"resource_type"`
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Permissions    []string  `json:"permissions"`
	GrantedAt      time.Time `json:"granted_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type userExtendedAccessResponse struct {
	Items []userGrant `json:"items"`
	Links HALLinks    `json:"_links"`
}

// GetMyProjects returns the list of projects accessible to the current user.
func (c *Client) GetMyProjects(ctx context.Context) ([]*ProjectInfo, error) {
	// Get the current user's ID
	meURL, err := c.resolveURL("users/me")
	if err != nil {
		return nil, err
	}

	var me struct {
		ID string `json:"id"`
	}
	if err := c.getResource(ctx, meURL.String(), &me); err != nil {
		return nil, err
	}

	// Get user extended access (project grants)
	accessURL, err := c.resolveURL("users/" + url.PathEscape(me.ID) + "/extended-access?filter[resource_type]=project")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, accessURL.String(), http.NoBody)
	if err != nil {
		return nil, Error{Original: err, URL: accessURL.String()}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, Error{Original: err, URL: accessURL.String(), Response: resp}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, Error{Response: resp, URL: accessURL.String()}
	}

	var accessResp userExtendedAccessResponse
	if err := json.NewDecoder(resp.Body).Decode(&accessResp); err != nil {
		return nil, Error{Original: err, URL: accessURL.String()}
	}

	if len(accessResp.Items) == 0 {
		return []*ProjectInfo{}, nil
	}

	// Extract the HAL links for project and organization references
	// The API returns links like "ref:projects:0" and "ref:organizations:0"
	projectRefURL := extractHALLink(accessResp.Links, "ref:projects")
	orgRefURL := extractHALLink(accessResp.Links, "ref:organizations")

	// Fetch project references using the HAL link (which includes sig parameter)
	projects, err := c.getProjectRefsFromLink(ctx, projectRefURL)
	if err != nil {
		return nil, err
	}

	// Fetch organization references using the HAL link
	orgs, err := c.getOrgRefsFromLink(ctx, orgRefURL)
	if err != nil {
		return nil, err
	}

	// Combine project and organization data
	result := make([]*ProjectInfo, 0, len(projects))
	for _, project := range projects {
		if project == nil {
			continue
		}
		if orgRef, ok := orgs[project.OrganizationID]; ok {
			project.OrganizationRef = orgRef
		}
		result = append(result, project)
	}

	return result, nil
}
