package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setupTeams starts an API with an organization "acme", its projects "alphaprojectid" and "betaprojectid", and a team
// "team-dev" with the user "user-id-2" and the project "alphaprojectid".
func setupTeams(t *testing.T) *cmdFactory {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	myUserID := "my-user-id"
	orgID := "org-id-teams"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	org := makeOrg(orgID, "acme", "ACME Inc.", myUserID, "flexible")
	org.Capabilities = []string{"teams"}
	org.Links["members"] = mockapi.HALLink{HREF: "/organizations/" + orgID + "/members"}
	apiHandler.SetOrgs([]*mockapi.Org{org})
	apiHandler.SetOrgMembers(orgID, []*mockapi.OrgMember{
		{UserID: myUserID, Owner: true, Permissions: []string{}},
		{UserID: "user-id-2", Permissions: []string{}},
		{UserID: "user-id-3", Permissions: []string{}},
	})
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject("alphaprojectid", orgID, "test-vendor", "Alpha", "region-1"),
		makeProject("betaprojectid", orgID, "test-vendor", "Beta", "region-1"),
	})
	apiHandler.SetTeams([]*mockapi.Team{{
		ID:                 "team-dev",
		OrganizationID:     orgID,
		Label:              "Developers",
		ProjectPermissions: []string{"viewer", "development:contributor"},
	}})
	apiHandler.SetTeamMembers("team-dev", []*mockapi.TeamMember{{UserID: "user-id-2"}})
	apiHandler.SetTeamProjects("team-dev", []*mockapi.TeamProjectAccess{{
		OrganizationID: orgID,
		ProjectID:      "alphaprojectid",
		ProjectTitle:   "Alpha",
	}})

	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	return newCommandFactory(t, apiServer.URL, authServer.URL)
}

func TestTeamCreate(t *testing.T) {
	t.Parallel()
	f := setupTeams(t)

	stdout, stderr, err := f.RunCombinedOutput("team:create", "-o", "acme", "--label", "Ops",
		"-r", "viewer", "-r", "production:viewer", "-r", "dev%:contributor", "--output-id")
	require.NoError(t, err)
	teamID := strings.TrimSpace(stdout)
	require.NotEmpty(t, teamID)
	assert.Contains(t, stderr, "Created team Ops ("+teamID+") in the organization ACME Inc. (acme)")
	assertTrimmed(t, "- viewer\n- 'production:viewer'\n- 'development:contributor'",
		f.Run("team:get", "-t", teamID, "-P", "project_permissions"))

	// Without --output-id, the new team is displayed on stderr.
	stdout, stderr, err = f.RunCombinedOutput("team:create", "-o", "acme", "--label", "Admins", "-r", "admin")
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Viewing the team Admins")

	assertTrimmed(t, `
Admins	0	0	["admin"]
Developers	1	1	["viewer","development:contributor"]
Ops	0	0	["viewer","production:viewer","development:contributor"]
`, f.Run("teams", "-o", "acme", "--format", "plain", "--no-header",
		"--columns", "label,member_count,project_count,project_permissions"))
	assertTrimmed(t, "Developers\nOps\nAdmins", f.Run("teams", "-o", "acme", "--sort", "created_at",
		"--format", "plain", "--no-header", "--columns", "label"))
	assertTrimmed(t, "Admins\nOps\nDevelopers", f.Run("teams", "-o", "acme", "--sort", "created_at", "--reverse",
		"--format", "plain", "--no-header", "--columns", "label"))

	cases := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "duplicate label",
			args:       []string{"--label", "developers", "-r", "admin"},
			wantStderr: "Another team team-dev exists in the organization with the same label: developers",
		},
		{
			name:       "duplicate label without the check",
			args:       []string{"--label", "Developers", "-r", "admin", "--no-check-unique"},
			wantStderr: "A team already exists with the same label: Developers",
		},
		{
			name:       "viewer without environment types",
			args:       []string{"--label", "Viewers", "-r", "viewer"},
			wantStderr: "At least one environment type role must be specified when the project role is viewer.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, err := f.RunCombinedOutput(append([]string{"team:create", "-o", "acme"}, c.args...)...)
			assert.Error(t, err)
			assert.Contains(t, stderr, c.wantStderr)
		})
	}
}

func TestTeamGetUpdateDelete(t *testing.T) {
	t.Parallel()
	f := setupTeams(t)

	_, stderr, err := f.RunCombinedOutput("team:get", "-t", "team-dev")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Viewing the team Developers (team-dev) in the organization ACME Inc. (acme)")
	assertTrimmed(t, "org-id-teams", f.Run("team:get", "-t", "team-dev", "-P", "organization_id"))

	_, stderr, err = f.RunCombinedOutput("team:get", "-t", "team-missing")
	assert.Error(t, err)
	assert.Contains(t, stderr, "Team not found: team-missing")

	_, stderr, err = f.RunCombinedOutput("team:update", "-t", "team-dev", "--label", "Developers")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Nothing to update")

	_, stderr, err = f.RunCombinedOutput("team:update", "-t", "team-dev", "--label", "Engineers", "-r", "admin")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Label: Developers -> Engineers")
	assert.Contains(t, stderr, "Project role: viewer -> admin")
	assertTrimmed(t, "Engineers", f.Run("team:get", "-t", "team-dev", "-P", "label"))
	assertTrimmed(t, "- admin", f.Run("team:get", "-t", "team-dev", "-P", "project_permissions"))

	// Declining the confirmation keeps the team.
	_, _, err = f.RunCombinedOutput("team:delete", "-t", "team-dev", "--no")
	assert.Error(t, err)
	f.Run("team:get", "-t", "team-dev")

	_, stderr, err = f.RunCombinedOutput("team:delete", "-t", "team-dev", "--yes")
	require.NoError(t, err)
	assert.Contains(t, stderr, "The team Engineers (team-dev) was deleted.")

	_, stderr, err = f.RunCombinedOutput("team:get", "-t", "team-dev")
	assert.Error(t, err)
	assert.Contains(t, stderr, "Team not found: team-dev")
}

func TestTeamProject(t *testing.T) {
	t.Parallel()
	f := setupTeams(t)

	assertTrimmed(t, "alphaprojectid\tAlpha", f.Run("team:projects", "-t", "team-dev",
		"--format", "tsv", "--columns", "id,title", "--no-header"))

	// Teams on a project.
	assertTrimmed(t, "team-dev\tDevelopers", f.Run("teams", "-o", "acme", "-p", "alphaprojectid",
		"--format", "tsv", "--columns", "id,label", "--no-header"))
	_, stderr, err := f.RunCombinedOutput("teams", "-o", "acme", "-p", "betaprojectid")
	assert.Error(t, err)
	assert.Contains(t, stderr, "No teams found on the project Beta (betaprojectid).")

	_, stderr, err = f.RunCombinedOutput("team:project:add", "-t", "team-dev", "alphaprojectid")
	require.NoError(t, err)
	assert.Contains(t, stderr, "The team already has access to the project Alpha (alphaprojectid)")
	assert.Contains(t, stderr, "There are no projects to add.")

	_, stderr, err = f.RunCombinedOutput("team:project:add", "-t", "team-dev", "betaprojectid")
	require.NoError(t, err)
	assert.Contains(t, stderr, "add the project Beta (betaprojectid) to the team Developers (team-dev)?")
	assert.Contains(t, stderr, "The project(s) were successfully added to the team Developers (team-dev).")
	assertTrimmed(t, "alphaprojectid\nbetaprojectid", f.Run("team:projects", "-t", "team-dev",
		"--format", "tsv", "--columns", "id", "--no-header"))

	_, stderr, err = f.RunCombinedOutput("team:project:delete", "-t", "team-dev", "gammaprojectid")
	assert.Error(t, err)
	assert.Contains(t, stderr, "The project ID gammaprojectid was not found in the team Developers (team-dev).")

	_, stderr, err = f.RunCombinedOutput("team:project:delete", "-t", "team-dev", "alphaprojectid")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Alpha (alphaprojectid) was successfully removed from the team Developers (team-dev).")
	assertTrimmed(t, "betaprojectid", f.Run("team:projects", "-t", "team-dev",
		"--format", "tsv", "--columns", "id", "--no-header"))
}

func TestTeamUser(t *testing.T) {
	t.Parallel()
	f := setupTeams(t)

	assertTrimmed(t, "user-id-2\tuser-id-2@example.com", f.Run("team:users", "-t", "team-dev",
		"--format", "tsv", "--columns", "id,email", "--no-header"))

	_, stderr, err := f.RunCombinedOutput("team:user:add", "-t", "team-dev", "user-id-2@example.com")
	require.NoError(t, err)
	assert.Contains(t, stderr, "User user-id-2 (user-id-2@example.com) is already in the team Developers (team-dev).")

	_, stderr, err = f.RunCombinedOutput("team:user:add", "-t", "team-dev", "outsider@example.com")
	assert.Error(t, err)
	assert.Contains(t, stderr, "outsider@example.com was not found in the organization ACME Inc. (acme).")
	assert.Contains(t, stderr, "A team may only contain users who are part of the organization.")

	_, stderr, err = f.RunCombinedOutput("team:user:add", "-t", "team-dev", "user-id-3@example.com")
	require.NoError(t, err)
	assert.Contains(t, stderr, "The user was successfully added to the team Developers (team-dev).")

	// A user can also be added by ID.
	_, stderr, err = f.RunCombinedOutput("team:user:add", "-t", "team-dev", "my-user-id")
	require.NoError(t, err)
	assert.Contains(t, stderr, "The user was successfully added to the team Developers (team-dev).")
	assertTrimmed(t, "user-id-2\nuser-id-3\nmy-user-id", f.Run("team:users", "-t", "team-dev",
		"--format", "tsv", "--columns", "id", "--no-header"))

	_, stderr, err = f.RunCombinedOutput("team:user:delete", "-t", "team-dev", "user-id-9")
	assert.Error(t, err)
	assert.Contains(t, stderr, "The user user-id-9 was not found in the team Developers (team-dev)")

	_, stderr, err = f.RunCombinedOutput("team:user:delete", "-t", "team-dev", "user-id-2@example.com")
	require.NoError(t, err)
	assert.Contains(t, stderr, "(user-id-2@example.com) was successfully removed from the team Developers (team-dev).")

	_, stderr, err = f.RunCombinedOutput("team:user:delete", "-t", "team-dev", "user-id-2@example.com")
	assert.Error(t, err)
	assert.Contains(t, stderr, "user-id-2@example.com is not part of the team Developers (team-dev)")
	assertTrimmed(t, "user-id-3\nmy-user-id", f.Run("team:users", "-t", "team-dev",
		"--format", "tsv", "--columns", "id", "--no-header"))
}
