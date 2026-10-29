package tests

import (
	"net/http/httptest"
	"testing"

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
