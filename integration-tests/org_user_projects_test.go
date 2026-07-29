package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestOrgUserProjects verifies the path that PHPStan flags as potentially passing
// a nullable UserRef into Api::getUserRefLabel() and UserRef::email accesses in
// OrganizationUserProjectsCommand (lines 135, 137, 165, 171, 184).
//
// The org+email path uses Api::loadMemberByEmail(), which already filters out
// members whose getUserInfo() is null. The lookup we deliberately make miss for
// one member (by omitting its entry from the ref:users map in the members
// listing) exercises that filter. The remaining matched member always has a
// non-null UserRef at runtime, so the lookup in the command body cannot return
// null. The test confirms the command completes without a TypeError.
func TestOrgUserProjects(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "user-for-oups-test"
	targetUserID := "target-user-id"
	missingUserID := "missing-ref-user-id"
	orgID := "org-id-oups"
	orgName := "oups-org"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})

	org := makeOrg(orgID, orgName, "OUPS Org", myUserID, "flexible")
	org.Links["members"] = mockapi.HALLink{HREF: "/organizations/" + url.PathEscape(orgID) + "/members"}
	apiHandler.SetOrgs([]*mockapi.Org{org})

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject(projectID, orgID, "test-vendor", "Project 1", "region-1"),
	})

	apiHandler.SetUserGrants([]*mockapi.UserGrant{
		{
			ResourceID:     projectID,
			ResourceType:   "project",
			OrganizationID: orgID,
			UserID:         targetUserID,
			Permissions:    []string{"viewer"},
		},
	})

	// Mock the organization members listing. The "ref:users" map intentionally
	// omits missingUserID, so getUserInfo() returns null for that member and
	// loadMemberByEmail filters it out.
	apiHandler.Get("/organizations/"+orgID+"/members", func(w http.ResponseWriter, _ *http.Request) {
		body := map[string]any{
			"items": []map[string]any{
				{
					"id":              "member-1",
					"organization_id": orgID,
					"user_id":         targetUserID,
					"permissions":     []string{"members:viewer"},
					"owner":           false,
					"created_at":      "2024-01-01T00:00:00+00:00",
					"updated_at":      "2024-01-01T00:00:00+00:00",
				},
				{
					"id":              "member-2",
					"organization_id": orgID,
					"user_id":         missingUserID,
					"permissions":     []string{"members:viewer"},
					"owner":           false,
					"created_at":      "2024-01-01T00:00:00+00:00",
					"updated_at":      "2024-01-01T00:00:00+00:00",
				},
			},
			"_links": map[string]any{
				"self":        map[string]string{"href": "/organizations/" + orgID + "/members"},
				"ref:users:0": map[string]string{"href": "/ref/users?in=" + targetUserID},
				// Deliberately omit missingUserID from the ref:users lookup.
			},
		}
		_ = json.NewEncoder(w).Encode(body)
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// Run with org+email path; should complete successfully and find the project.
	stdout, stderr, err := f.RunCombinedOutput("oups", "-o", orgName, targetUserID+"@example.com")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stderr, "Project access for the user")
	assert.Contains(t, stdout, projectID)

	// Run with org+email for the user whose ref:users entry is missing.
	// loadMemberByEmail filters that member out, so the CLI reports "User not
	// found" and returns 1 — without ever assigning a nullable UserRef into
	// the code at lines 135/137/165/171/184.
	_, stderr, err = f.RunCombinedOutput("oups", "-o", orgName, missingUserID+"@example.com")
	require.Error(t, err)
	assert.Contains(t, stderr, "User not found for email address")

	// Also exercise the "no projects" branch (line 135), which is the literal
	// PHPStan finding's first occurrence. Use a user that exists but has no
	// project grants.
	apiHandler.SetUserGrants(nil)
	stdout, stderr, err = f.RunCombinedOutput("oups", "-o", orgName, targetUserID+"@example.com")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stderr, "No projects were found for the user")
	assert.True(t, strings.Contains(stderr, targetUserID+"@example.com"))
}
