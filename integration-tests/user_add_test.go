package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestUserAddInteractive(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID := setupProjectUsers(t)

	// The email is asked again if invalid, the project role defaults to viewer, and "q" skips the other types.
	_, stderr, err := f.RunInteractive("bad\nnew@example.com\n\nc\nq\ny\n", "user:add", "-p", projectID)
	require.NoError(t, err)
	assert.Contains(t, stderr, "Enter the user's email address: Invalid email address: bad")
	assert.Contains(t, stderr, "The user's project role can be admin (a) or viewer (v).")
	assert.Contains(t, stderr, "Project role (default: viewer) [a/v]:")
	assert.Contains(t, stderr,
		"The user's environment type role(s) can be admin (a), contributor (c), viewer (v) or none (n).")
	assert.Contains(t, stderr, "Role on type development (default: none) [a/c/v/n]:")
	assert.NotContains(t, stderr, "Role on type staging")
	assert.Contains(t, stderr, "Adding the user new@example.com to Project 1")
	assert.Contains(t, stderr, "An invitation has been sent to new@example.com")
	invitations := apiHandler.ProjectInvitations(projectID)
	require.Len(t, invitations, 1)
	assert.Equal(t, "viewer", invitations[0].Role)
	assert.Equal(t, []mockapi.InvitationPermission{{Type: "development", Role: "contributor"}},
		invitations[0].Permissions)

	// An existing user's roles are the defaults, so accepting them makes no changes.
	_, stderr, err = f.RunInteractive("\n\n\n\n", "user:add", "-p", projectID, "user-id-2@example.com")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Current role(s) of User user-id-2 (user-id-2@example.com) on Project 1")
	assert.Contains(t, stderr, "Role on type development (default: viewer) [a/c/v/n]:")
	assert.Contains(t, stderr, "There are no changes to make.")

	// Without an email address, user:update lists the users to choose from.
	_, stderr, err = f.RunInteractive("1\n\n\na\n\ny\n", "user:update", "-p", projectID)
	require.NoError(t, err)
	assert.Contains(t, stderr, "Enter a number to choose a user to update:")
	assert.Contains(t, stderr, "[0] User my-user-id (my-user-id@example.com)")
	assert.Contains(t, stderr, "[1] User user-id-2 (user-id-2@example.com)")
	assert.Contains(t, stderr, "Role on type production: none -> admin")
	assert.Contains(t, stderr, "Access was updated successfully.")
	assert.Equal(t, "admin\n", f.Run("user:get", "-p", projectID, "-e", "main", "user-id-2@example.com",
		"--level", "environment", "--pipe"))
	assert.Equal(t, "viewer\n", f.Run("user:get", "-p", projectID, "-e", "dev", "user-id-2@example.com",
		"--level", "environment", "--pipe"))
}

func TestUserAddForceInvite(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID := setupProjectUsers(t)

	f.Run("user:add", "-p", projectID, "new@example.com", "-r", "viewer", "-r", "staging:viewer")

	// A forced invitation replaces the pending one without asking.
	_, stderr, err := f.RunCombinedOutput("user:add", "-p", projectID, "new@example.com", "-r", "admin",
		"--force-invite")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "An invitation has already been sent")
	assert.Contains(t, stderr, "An invitation has been sent to new@example.com")
	invitations := apiHandler.ProjectInvitations(projectID)
	require.Len(t, invitations, 2)
	assert.Equal(t, "cancelled", invitations[0].State) //nolint:misspell // The API's spelling.
	assert.Equal(t, "pending", invitations[1].State)
	assert.Equal(t, "admin", invitations[1].Role)
}

func TestUserAddOwner(t *testing.T) {
	t.Parallel()
	f, _, projectID := setupProjectUsers(t)

	_, stderr, err := f.RunCombinedOutput("user:add", "-p", projectID, "my-user-id@example.com")
	require.NoError(t, err)
	assert.Contains(t, stderr, "The user User my-user-id (my-user-id@example.com) is the owner of Project 1")
	assert.NotContains(t, stderr, "cannot be changed")
}

func TestUserAddDeclined(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID := setupProjectUsers(t)

	_, stderr, err := f.RunCombinedOutput("user:add", "-p", projectID, "new@example.com", "-r", "admin", "--no")
	assert.Error(t, err)
	assert.Contains(t, stderr, "Are you sure you want to add this user? [Y/n] n")
	assert.Empty(t, apiHandler.ProjectInvitations(projectID))

	_, stderr, err = f.RunCombinedOutput("user:update", "-p", projectID, "user-id-2@example.com", "-r", "admin", "--no")
	assert.Error(t, err)
	assert.Contains(t, stderr, "Are you sure you want to make these change(s)? [Y/n] n")
	assert.Equal(t, "viewer\n", f.Run("user:get", "-p", projectID, "user-id-2@example.com", "--pipe"))

	// Removing an existing user's last environment type role is refused.
	_, stderr, err = f.RunCombinedOutput("user:update", "-p", projectID, "user-id-2@example.com",
		"-r", "development:none")
	assert.Error(t, err)
	assert.Contains(t, stderr, "A non-admin user must be added to at least one environment type.")
	assert.Contains(t, stderr, "To delete the user, run: platform-test user:delete user-id-2@example.com")
	assert.Empty(t, apiHandler.Requests("PATCH", "/projects/*/user-access/*"))
}

// TestUserAddEnvironmentRoles checks that roles given on environment IDs, rather than types, are converted.
func TestUserAddEnvironmentRoles(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID := setupProjectUsers(t)
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
		makeEnv(projectID, "dev", "development", "active", "main"),
		makeEnv(projectID, "feature", "development", "active", "dev"),
	})

	_, stderr, err := f.RunCombinedOutput("user:add", "-p", projectID, "new@example.com",
		"-r", "viewer", "-r", "dev:contributor", "-r", "main:viewer", "-r", "production:viewer")
	require.NoError(t, err)
	assert.Contains(t, stderr, "No environment type IDs match: dev")
	assert.Contains(t, stderr, "Access control is now based on environment types, not individual environments.")
	assert.Contains(t, stderr, "The role contributor specified on 1 environment(s) will actually be applied"+
		" to all existing and future environments of the same type, development.")
	assert.Contains(t, stderr, "The role viewer specified on 1 environment(s) will be ignored"+
		" as it is already specified on their type, production.")
	invitations := apiHandler.ProjectInvitations(projectID)
	require.Len(t, invitations, 1)
	assert.ElementsMatch(t, []mockapi.InvitationPermission{
		{Type: "development", Role: "contributor"},
		{Type: "production", Role: "viewer"},
	}, invitations[0].Permissions)

	cases := []struct {
		name       string
		args       []string
		wantStderr []string
	}{
		{
			name: "conflicting environment roles",
			args: []string{"-r", "viewer", "-r", "dev:contributor", "-r", "feature:viewer"},
			wantStderr: []string{
				"Conflicting roles were given for environments of type development:",
				"    dev: contributor\n    feature: viewer",
			},
		},
		{
			name: "conflicting type role",
			args: []string{"-r", "viewer", "-r", "main:admin", "-r", "production:viewer"},
			wantStderr: []string{"The role admin specified on 1 environment(s) conflicts with the role viewer" +
				" specified for the environment type production."},
		},
		{
			name:       "no match",
			args:       []string{"-r", "viewer", "-r", "nothing:viewer"},
			wantStderr: []string{"No environment type IDs match: nothing", "No environment IDs match: nothing"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, err := f.RunCombinedOutput(append([]string{"user:add", "-p", projectID, "other@example.com"},
				c.args...)...)
			assert.Error(t, err)
			for _, s := range c.wantStderr {
				assert.Contains(t, stderr, s)
			}
		})
	}

	// In interactive mode, the conversion is suggested instead.
	_, stderr, err = f.RunInteractive("", "user:add", "-p", projectID, "other@example.com",
		"-r", "viewer", "-r", "dev:contributor")
	assert.Error(t, err)
	assert.Contains(t, stderr, "Please use the environment type to specify roles.")
	assert.Contains(t, stderr, "For example, use:\n  --role viewer,development:contributor")
	assert.Len(t, apiHandler.ProjectInvitations(projectID), 1)
}
