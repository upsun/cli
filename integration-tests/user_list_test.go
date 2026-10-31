package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestUserList(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	projectID := mockapi.ProjectID()
	myUserID := "my-user-id"
	vendor := "test-vendor"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	org := makeOrg("org-id-1", "org-1", "Org 1", myUserID, "flexible")
	apiHandler.SetOrgs([]*mockapi.Org{org})
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject(projectID, "org-id-1", vendor, "Project 1", "region-1"),
	})
	apiHandler.SetUserGrants([]*mockapi.UserGrant{
		{
			ResourceID:     "org-id-1",
			ResourceType:   "organization",
			OrganizationID: "org-id-1",
			UserID:         myUserID,
			Permissions:    []string{"admin"},
		},
		{
			ResourceID:     projectID,
			ResourceType:   "project",
			OrganizationID: "org-id-1",
			UserID:         myUserID,
			Permissions:    []string{"admin"},
		},
		{
			ResourceID:     projectID,
			ResourceType:   "project",
			OrganizationID: "org-id-1",
			UserID:         "user-id-2",
			Permissions:    []string{"viewer", "development:viewer"},
		},
		{
			ResourceID:     projectID,
			ResourceType:   "project",
			OrganizationID: "org-id-1",
			UserID:         "user-id-3",
			Permissions:    []string{"viewer", "production:viewer", "development:admin", "staging:contributor"},
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assertTrimmed(t, `
+------------------------+-----------------+--------------+------------+
| Email address          | Name            | Project role | ID         |
+------------------------+-----------------+--------------+------------+
| my-user-id@example.com | User my-user-id | admin        | my-user-id |
| user-id-2@example.com  | User user-id-2  | viewer       | user-id-2  |
| user-id-3@example.com  | User user-id-3  | viewer       | user-id-3  |
+------------------------+-----------------+--------------+------------+
`, f.Run("users", "-p", projectID))

	assertTrimmed(t, `
Email address	Name	Project role	ID	Permissions
my-user-id@example.com	User my-user-id	admin	my-user-id	admin
user-id-2@example.com	User user-id-2	viewer	user-id-2	viewer, development:viewer
user-id-3@example.com	User user-id-3	viewer	user-id-3	viewer, production:viewer, development:admin, staging:contributor
`, f.Run("users", "-p", projectID, "--format", "plain", "--columns", "+perm%"))

	// With teams, the command suggests listing them. A new factory avoids the cached organization.
	org.Capabilities = []string{"teams"}
	org.Links["members"] = mockapi.HALLink{HREF: "/organizations/org-id-1/members"}
	apiHandler.SetOrgs([]*mockapi.Org{org})
	f = newCommandFactory(t, apiServer.URL, authServer.URL)
	_, stderr, err := f.RunCombinedOutput("users", "-p", projectID)
	require.NoError(t, err)
	assert.Contains(t, stderr, "To list teams with access to the project, run: platform-test teams -p "+projectID)
}
