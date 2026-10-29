package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setupProjectUsers starts an API with a project owned by "my-user-id", which also has the user "user-id-2".
func setupProjectUsers(t *testing.T) (f *cmdFactory, apiHandler *mockapi.Handler, projectID string) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	projectID = mockapi.ProjectID()
	myUserID := "my-user-id"

	apiHandler = mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	apiHandler.SetOrgs([]*mockapi.Org{makeOrg("org-id-1", "org-1", "Org 1", myUserID, "flexible")})
	project := makeProject(projectID, "org-id-1", "test-vendor", "Project 1", "region-1")
	project.Owner = myUserID
	apiHandler.SetProjects([]*mockapi.Project{project})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
		makeEnv(projectID, "dev", "development", "active", "main"),
	})
	apiHandler.SetUserGrants([]*mockapi.UserGrant{
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
	})

	return newCommandFactory(t, apiServer.URL, authServer.URL), apiHandler, projectID
}

func TestUserAdd(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID := setupProjectUsers(t)

	_, stderr, err := f.RunCombinedOutput("user:add", "-p", projectID, "new@example.com",
		"-r", "viewer", "-r", "production:contributor")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Adding the user new@example.com to Project 1")
	assert.Contains(t, stderr, "Role on type production: contributor")
	assert.Contains(t, stderr, "An invitation has been sent to new@example.com")

	invitations := apiHandler.ProjectInvitations(projectID)
	require.Len(t, invitations, 1)
	assert.Equal(t, "new@example.com", invitations[0].Email)
	assert.Equal(t, "viewer", invitations[0].Role)
	assert.Equal(t, []mockapi.InvitationPermission{{Type: "production", Role: "contributor"}}, invitations[0].Permissions)

	// Inviting the user again is confirmed, and forced.
	_, stderr, err = f.RunCombinedOutput("user:add", "-p", projectID, "new@example.com", "-r", "admin")
	require.NoError(t, err)
	assert.Contains(t, stderr, "An invitation has already been sent to new@example.com")
	assert.Contains(t, stderr, "A new invitation has been sent to new@example.com")
	invitations = apiHandler.ProjectInvitations(projectID)
	require.Len(t, invitations, 2)
	assert.Equal(t, "admin", invitations[1].Role)

	cases := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "invalid email",
			args:       []string{"not-an-email", "-r", "admin"},
			wantStderr: "Invalid email address: not-an-email",
		},
		{
			name:       "viewer without environment types",
			args:       []string{"viewer@example.com", "-r", "viewer"},
			wantStderr: "A non-admin user must be added to at least one environment type.",
		},
		{
			name:       "admin with environment type roles",
			args:       []string{"admin@example.com", "-r", "admin", "-r", "production:viewer"},
			wantStderr: "A project admin has administrative access to all environment types.",
		},
		{
			name:       "invalid role",
			args:       []string{"role@example.com", "-r", "owner"},
			wantStderr: "Invalid role: owner",
		},
		{
			name:       "empty environment type role",
			args:       []string{"empty-type-role@example.com", "-r", "viewer", "-r", "production:"},
			wantStderr: "A role cannot be empty.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, err := f.RunCombinedOutput(append([]string{"user:add", "-p", projectID}, c.args...)...)
			assert.Error(t, err)
			assert.Contains(t, stderr, c.wantStderr)
		})
	}
	assert.Len(t, apiHandler.ProjectInvitations(projectID), 2)
}

func TestUserUpdate(t *testing.T) {
	t.Parallel()
	f, _, projectID := setupProjectUsers(t)

	// Environment type roles are merged with the existing ones.
	_, stderr, err := f.RunCombinedOutput("user:update", "-p", projectID, "user-id-2@example.com",
		"-r", "production:admin")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Project role: viewer")
	assert.Contains(t, stderr, "Role on environment type development: viewer")
	assert.Contains(t, stderr, "Role on type production: none -> admin")
	assert.Contains(t, stderr, "Access was updated successfully.")
	assert.Equal(t, "admin\n", f.Run("user:get", "-p", projectID, "-e", "main", "user-id-2@example.com",
		"--level", "environment", "--pipe"))
	assert.Equal(t, "viewer\n", f.Run("user:get", "-p", projectID, "-e", "dev", "user-id-2@example.com",
		"--level", "environment", "--pipe"))

	// An empty role is rejected, not completed to the first role.
	_, stderr, err = f.RunCombinedOutput("user:update", "-p", projectID, "user-id-2@example.com", "-r", "development:")
	assert.Error(t, err)
	assert.Contains(t, stderr, "A role cannot be empty.")
	assert.Equal(t, "viewer\n", f.Run("user:get", "-p", projectID, "-e", "dev", "user-id-2@example.com",
		"--level", "environment", "--pipe"))

	// Changing the project role to admin removes the environment type roles.
	_, stderr, err = f.RunCombinedOutput("user:update", "-p", projectID, "user-id-2@example.com", "-r", "admin")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Project role: viewer -> admin")
	assertTrimmed(t, "my-user-id@example.com,admin\nuser-id-2@example.com,admin",
		f.Run("users", "-p", projectID, "--format", "csv", "--columns", "email,permissions", "--no-header"))

	// Nothing changes.
	_, stderr, err = f.RunCombinedOutput("user:update", "-p", projectID, "user-id-2@example.com", "-r", "admin")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "Access was updated")

	_, stderr, err = f.RunCombinedOutput("user:update", "-p", projectID, "nobody@example.com", "-r", "admin")
	assert.Error(t, err)
	assert.Contains(t, stderr, "User not found: nobody@example.com")

	// The project owner's role cannot be changed.
	_, stderr, err = f.RunCombinedOutput("user:update", "-p", projectID, "my-user-id@example.com", "-r", "viewer")
	assert.Error(t, err)
	assert.Contains(t, stderr, "The user User my-user-id (my-user-id@example.com) is the owner of Project 1")
	assert.Contains(t, stderr, "The project owner's role(s) cannot be changed.")
}

func TestUserDelete(t *testing.T) {
	t.Parallel()
	f, _, projectID := setupProjectUsers(t)

	_, stderr, err := f.RunCombinedOutput("user:delete", "-p", projectID, "my-user-id@example.com")
	assert.Error(t, err)
	assert.Contains(t, stderr, "The user my-user-id@example.com is the owner of the project Project 1")
	assert.Contains(t, stderr, "The project's owner cannot be deleted.")

	_, stderr, err = f.RunCombinedOutput("user:delete", "-p", projectID, "nobody@example.com")
	assert.Error(t, err)
	assert.Contains(t, stderr, "User not found: nobody@example.com")

	// Declining the confirmation keeps the user.
	_, _, err = f.RunCombinedOutput("user:delete", "-p", projectID, "user-id-2@example.com", "--no")
	assert.Error(t, err)
	assert.Equal(t, "viewer\n", f.Run("user:get", "-p", projectID, "user-id-2@example.com", "--pipe"))

	_, stderr, err = f.RunCombinedOutput("user:delete", "-p", projectID, "user-id-2@example.com")
	require.NoError(t, err)
	assert.Contains(t, stderr, "User user-id-2@example.com deleted")

	_, stderr, err = f.RunCombinedOutput("user:get", "-p", projectID, "user-id-2@example.com", "--pipe")
	assert.Error(t, err)
	assert.Contains(t, stderr, "User not found: user-id-2@example.com")
	assert.NotContains(t, f.Run("users", "-p", projectID, "--format", "plain"), "user-id-2")
}
