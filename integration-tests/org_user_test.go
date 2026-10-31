package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestOrgUser(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"
	orgID := "org-id-users"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	org := makeOrg(orgID, "acme", "ACME Inc.", myUserID, "flexible")
	org.Links["members"] = mockapi.HALLink{HREF: "/organizations/" + orgID + "/members"}
	org.Links["invitations"] = mockapi.HALLink{HREF: "/organizations/" + orgID + "/invitations"}
	apiHandler.SetOrgs([]*mockapi.Org{org})
	apiHandler.SetOrgMembers(orgID, []*mockapi.OrgMember{
		{UserID: myUserID, Owner: true, Permissions: []string{}},
		{UserID: "user-id-2", Permissions: []string{"billing"}},
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assertTrimmed(t, `
my-user-id,my-user-id@example.com,true,
user-id-2,user-id-2@example.com,false,billing
`, f.Run("org:user:list", "-o", "acme", "--format", "csv", "--no-header"))

	assertTrimmed(t, "- billing", f.Run("org:user:get", "-o", "acme", "user-id-2@example.com", "-P", "permissions"))

	t.Run("add", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("org:user:add", "-o", "acme", "new@example.com",
			"--permission", "billing", "--permission", "members")
		require.NoError(t, err)
		assert.Contains(t, stderr, "Are you sure you want to invite new@example.com to the organization ACME Inc. (acme)?")
		assert.Contains(t, stderr, "The user has been successfully invited to the organization.")
		invitations := apiHandler.OrgInvitations(orgID)
		require.Len(t, invitations, 1)
		assert.Equal(t, "new@example.com", invitations[0].Email)
		assert.Equal(t, []string{"billing", "members"}, invitations[0].Permissions)

		_, stderr, err = f.RunCombinedOutput("org:user:add", "-o", "acme", "user-id-2@example.com")
		assert.Error(t, err)
		assert.Contains(t, stderr, "The user user-id-2@example.com already exists on the organization ACME Inc. (acme)")
		assert.Len(t, apiHandler.OrgInvitations(orgID), 1)
	})

	t.Run("update", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("org:user:update", "-o", "acme", "user-id-2@example.com",
			"--permission", "billing")
		require.NoError(t, err)
		assert.Contains(t, stderr, "The user's permissions are already set to: billing")

		_, stderr, err = f.RunCombinedOutput("org:user:update", "-o", "acme", "user-id-2@example.com",
			"--permission", "members", "--permission", "plans")
		require.NoError(t, err)
		assert.Contains(t, stderr, "- billing")
		assert.Contains(t, stderr, "+ members")
		assert.Contains(t, stderr, "+ plans")
		assert.Contains(t, stderr, "The user's permissions are now: members, plans")
		assertTrimmed(t, "- members\n- plans",
			f.Run("org:user:get", "-o", "acme", "user-id-2@example.com", "-P", "permissions"))
		patches := len(apiHandler.Requests("PATCH", "/organizations/*/members/*"))

		// The same permissions in another order are not a change.
		_, stderr, err = f.RunCombinedOutput("org:user:update", "-o", "acme", "user-id-2@example.com",
			"--permission", "plans,members")
		require.NoError(t, err)
		assert.Contains(t, stderr, "The user's permissions are already set to: members, plans")

		// Without --permission, the permissions are not replaced with none.
		_, stderr, err = f.RunCombinedOutput("org:user:update", "-o", "acme", "user-id-2@example.com")
		assert.Error(t, err)
		assert.Contains(t, stderr, "The --permission option is required in non-interactive mode.")
		assert.Len(t, apiHandler.Requests("PATCH", "/organizations/*/members/*"), patches)

		// An explicit empty value removes the permissions.
		_, stderr, err = f.RunCombinedOutput("org:user:update", "-o", "acme", "user-id-2@example.com", "--permission", "")
		require.NoError(t, err)
		assert.Contains(t, stderr, "- members")
		assert.Contains(t, stderr, "The user's permissions are now: [none]")

		_, stderr, err = f.RunCombinedOutput("org:user:update", "-o", "acme", "my-user-id@example.com",
			"--permission", "billing")
		assert.Error(t, err)
		assert.Contains(t, stderr, "The user is the owner of the organization, so does not need permissions.")
	})

	t.Run("delete", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("org:user:delete", "-o", "acme", "user-id-2@example.com", "--no")
		assert.Error(t, err)
		assert.Contains(t, stderr, "delete the user user-id-2@example.com from the organization ACME Inc. (acme)? [Y/n] n")
		f.Run("org:user:get", "-o", "acme", "user-id-2@example.com")

		_, stderr, err = f.RunCombinedOutput("org:user:delete", "-o", "acme", "user-id-2@example.com")
		require.NoError(t, err)
		assert.Contains(t, stderr, "The user was successfully deleted.")

		_, stderr, err = f.RunCombinedOutput("org:user:get", "-o", "acme", "user-id-2@example.com")
		assert.Error(t, err)
		assert.Contains(t, stderr, "User not found: user-id-2@example.com")
		assertTrimmed(t, "my-user-id",
			f.Run("org:user:list", "-o", "acme", "--format", "csv", "--columns", "id", "--no-header"))
	})
}

// setupOrgUsers starts an API with the organization "acme", owned by "my-user-id", which also has the member
// "user-id-2" with the "billing" permission.
func setupOrgUsers(t *testing.T) (f *cmdFactory, apiHandler *mockapi.Handler, org *mockapi.Org) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	myUserID := "my-user-id"
	orgID := "org-id-acme"
	apiHandler = mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID, Email: myUserID + "@example.com"})
	org = makeOrg(orgID, "acme", "ACME Inc.", myUserID, "flexible")
	org.Links["members"] = mockapi.HALLink{HREF: "/organizations/" + orgID + "/members"}
	org.Links["invitations"] = mockapi.HALLink{HREF: "/organizations/" + orgID + "/invitations"}
	apiHandler.SetOrgs([]*mockapi.Org{org})
	apiHandler.SetOrgMembers(orgID, []*mockapi.OrgMember{
		{UserID: myUserID, Owner: true, Permissions: []string{}},
		{UserID: "user-id-2", Permissions: []string{"billing"}},
	})
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	return newCommandFactory(t, apiServer.URL, authServer.URL), apiHandler, org
}

func TestOrgUserInteractive(t *testing.T) {
	t.Parallel()
	f, apiHandler, org := setupOrgUsers(t)

	t.Run("get", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("org:user:get", "-o", "acme")
		assert.Error(t, err)
		assert.Contains(t, stderr, "You must specify the email address of a user to view (in non-interactive mode).")

		stdout, stderr, err := f.RunInteractive("1\n", "org:user:get", "-o", "acme", "-P", "user_id")
		require.NoError(t, err)
		assert.Contains(t, stderr, "[0] User my-user-id (my-user-id@example.com) (owner - default)")
		assert.Contains(t, stderr, "[1] User user-id-2 (user-id-2@example.com)")
		assert.Equal(t, "user-id-2\n", stdout)
	})

	t.Run("update", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("org:user:update", "-o", "acme")
		assert.Error(t, err)
		assert.Contains(t, stderr,
			"You must specify the email address of a user to update (in non-interactive mode).")

		// An unrecognized permission is asked again.
		_, stderr, err = f.RunInteractive("1\nfoo\nbilling,members\ny\n", "org:user:update", "-o", "acme")
		require.NoError(t, err)
		assert.Contains(t, stderr, "A user may have any of the following permissions: ")
		assert.Contains(t, stderr,
			"The user user-id-2@example.com currently has the following permissions: billing.")
		assert.Contains(t, stderr, "Unrecognized permission: foo")
		assert.Contains(t, stderr, "  Permissions:\n      billing\n    + members\n")
		assert.Contains(t, stderr, "The user's permissions are now: billing, members")
		assertTrimmed(t, "- billing\n- members",
			f.Run("org:user:get", "-o", "acme", "user-id-2@example.com", "-P", "permissions"))

		_, stderr, err = f.RunCombinedOutput("org:user:update", "-o", "acme", "user-id-2@example.com",
			"--permission", "members", "--no")
		assert.Error(t, err)
		assert.Contains(t, stderr, "Are you sure you want to make these changes? [Y/n] n")
		assertTrimmed(t, "- billing\n- members",
			f.Run("org:user:get", "-o", "acme", "user-id-2@example.com", "-P", "permissions"))
	})

	t.Run("add", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("org:user:add", "-o", "acme")
		assert.Error(t, err)
		assert.Contains(t, stderr, "A user email address is required.")

		_, stderr, err = f.RunCombinedOutput("org:user:add", "-o", "acme", "not-an-email")
		assert.Error(t, err)
		assert.Contains(t, stderr, "Invalid email address: not-an-email")

		_, stderr, err = f.RunCombinedOutput("org:user:add", "-o", "acme", "declined@example.com",
			"--permission", "billing", "--no")
		assert.Error(t, err)
		assert.Contains(t, stderr, "Are you sure you want to invite declined@example.com to the organization")
		assert.Empty(t, apiHandler.OrgInvitations(org.ID))

		// Without permissions, the user is invited with none.
		_, stderr, err = f.RunInteractive("\nbad\nnew@example.com\n\ny\n", "org:user:add", "-o", "acme")
		require.NoError(t, err)
		assert.Contains(t, stderr, "An email address is required.")
		assert.Contains(t, stderr, "Invalid email address: bad")
		assert.Contains(t, stderr, "Optionally, enter a list of permissions to add (separated by commas)")
		assert.Contains(t, stderr, "The user has been successfully invited to the organization.")
		invitations := apiHandler.OrgInvitations(org.ID)
		require.Len(t, invitations, 1)
		assert.Equal(t, "new@example.com", invitations[0].Email)
		assert.Empty(t, invitations[0].Permissions)
	})

	t.Run("projects", func(t *testing.T) {
		projectID := mockapi.ProjectID()
		apiHandler.SetProjects([]*mockapi.Project{
			makeProject(projectID, org.ID, "test-vendor", "Project 1", "region-1"),
		})
		apiHandler.SetUserGrants([]*mockapi.UserGrant{{
			ResourceID:     projectID,
			ResourceType:   "project",
			OrganizationID: org.ID,
			UserID:         "user-id-2",
			Permissions:    []string{"admin"},
		}})

		_, stderr, err := f.RunCombinedOutput("org:user:projects", "-o", "acme")
		assert.Error(t, err)
		assert.Contains(t, stderr, "A user email address is required.")

		stdout, stderr, err := f.RunInteractive("1\n", "org:user:projects", "-o", "acme",
			"--format", "plain", "--columns", "project_id", "--no-header")
		require.NoError(t, err)
		assert.Contains(t, stderr, "Enter a number to choose a user:")
		assertTrimmed(t, projectID, stdout)
	})
}

// TestOrgUserAddInvitationState checks the result of an invitation that is not pending.
func TestOrgUserAddInvitationState(t *testing.T) {
	t.Parallel()
	f, apiHandler, org := setupOrgUsers(t)

	cases := []struct {
		state      string
		wantErr    bool
		wantStderr string
	}{
		{state: "accepted", wantStderr: "The user has been successfully added to the organization."},
		//nolint:misspell // The API's spelling.
		{state: "cancelled", wantErr: true, wantStderr: "The invitation inv-1 was cancelled."},
		{state: "error", wantErr: true, wantStderr: "The invitation inv-1 errored."},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			apiHandler.Post("/organizations/"+org.ID+"/invitations", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "inv-1", "state": c.state, "organization_id": org.ID, "email": "new@example.com",
				})
			})
			_, stderr, err := f.RunCombinedOutput("org:user:add", "-o", "acme", "new@example.com",
				"--permission", "billing")
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Contains(t, stderr, c.wantStderr)
		})
	}
}

func TestOrgUserList(t *testing.T) {
	t.Parallel()
	f, apiHandler, org := setupOrgUsers(t)

	// Members are created in order, and updated in reverse order.
	members := make([]*mockapi.OrgMember, 101)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range members {
		members[i] = &mockapi.OrgMember{
			UserID:      fmt.Sprintf("user-%03d", i),
			Owner:       i == 0,
			Permissions: []string{},
			CreatedAt:   base.Add(time.Duration(i) * time.Hour),
			UpdatedAt:   base.Add(-time.Duration(i) * time.Hour),
		}
	}
	apiHandler.SetOrgMembers(org.ID, members)

	ids := func(n int) string {
		s := make([]string, 0, n)
		for i := range n {
			s = append(s, fmt.Sprintf("user-%03d", i))
		}
		return strings.Join(s, "\n")
	}

	stdout, stderr, err := f.RunCombinedOutput("org:user:list", "-o", "acme")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Users in the organization ACME Inc. (acme):")
	assert.Contains(t, stdout, "| user-019 | user-019@example.com | false  |")
	assert.NotContains(t, stdout, "user-020")
	assert.Contains(t, stderr, "More users are available (displaying 20, total 101)")
	assert.Contains(t, stderr, "Show all users with: --count 0 (-c0)")
	assert.Contains(t, stderr, "To get full user details, run: platform-test org:user:get --org acme [email]")
	assert.Contains(t, stderr, "To remove a user, run: platform-test org:user:delete --org acme [email]")

	list := func(args ...string) string {
		return strings.TrimSpace(f.Run(append([]string{"org:user:list", "-o", "acme", "--format", "plain",
			"--columns", "id", "--no-header"}, args...)...))
	}
	assert.Equal(t, ids(3), list("--count", "3"))
	assert.Equal(t, ids(101), list("--count", "0"))
	assert.Equal(t, "user-100\nuser-099", list("--count", "2", "--reverse"))
	assert.Equal(t, "user-100\nuser-099", list("--count", "2", "--sort", "updated_at"))
	assert.Equal(t, "user-000\nuser-001", list("--count", "2", "--sort", "updated_at", "--reverse"))

	for _, count := range []string{"abc", "101", "-1", "1.5", "1e1"} {
		_, stderr, err := f.RunCombinedOutput("org:user:list", "-o", "acme", "--count="+count)
		assert.Error(t, err)
		assert.Contains(t, stderr, "The --count must be a number between 1 and 100, or 0 to disable pagination.")
	}

	apiHandler.SetOrgMembers(org.ID, nil)
	_, stderr, err = f.RunCombinedOutput("org:user:list", "-o", "acme")
	assert.Error(t, err)
	assert.Contains(t, stderr, "No users found")
}

// TestOrgUserProjectsListAll checks project access across organizations.
func TestOrgUserProjectsListAll(t *testing.T) {
	t.Parallel()
	f, apiHandler, org := setupOrgUsers(t)
	otherOrg := makeOrg("org-id-other", "other", "Other Org", "someone-else", "flexible")
	apiHandler.SetOrgs([]*mockapi.Org{org, otherOrg})

	projectID1, projectID2 := mockapi.ProjectID(), mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject(projectID1, org.ID, "test-vendor", "Project 1", "region-1"),
		makeProject(projectID2, otherOrg.ID, "test-vendor", "Project 2", "region-1"),
	})
	apiHandler.SetUserGrants([]*mockapi.UserGrant{
		{ResourceID: projectID1, ResourceType: "project", OrganizationID: org.ID, UserID: "user-id-2",
			Permissions: []string{"viewer", "production:viewer"}},
		{ResourceID: projectID2, ResourceType: "project", OrganizationID: otherOrg.ID, UserID: "user-id-2",
			Permissions: []string{"admin"}},
	})

	assertTrimmed(t, projectID1, f.Run("org:user:projects", "-o", "acme", "user-id-2@example.com",
		"--format", "plain", "--columns", "project_id", "--no-header"))

	stdout, stderr, err := f.RunCombinedOutput("org:user:projects", "--list-all", "user-id-2@example.com")
	require.NoError(t, err)
	assert.Contains(t, stderr, "All project access for the user User user-id-2 <user-id-2@example.com>:")
	assert.Contains(t, stdout, "| Organization |")
	assert.Contains(t, stdout, "| Project: admin ")
	assertTrimmed(t, projectID1+",acme\n"+projectID2+",other", f.Run("org:user:projects", "--list-all",
		"user-id-2@example.com", "--format", "csv", "--columns", "project_id,organization_name", "--no-header"))
	assert.Contains(t, stderr, "To view the user details, run: platform-test org:user:get user-id-2@example.com")

	_, stderr, err = f.RunCombinedOutput("org:user:projects", "--list-all", "nobody@example.com")
	require.NoError(t, err)
	assert.Contains(t, stderr, "No projects were found for the user User nobody <nobody@example.com>.")

	_, stderr, err = f.RunCombinedOutput("org:user:projects", "-o", "other", "user-id-2@example.com")
	assert.Error(t, err)
	assert.Contains(t, stderr, "You do not have permission to view users in the organization Other Org (other).")
}
