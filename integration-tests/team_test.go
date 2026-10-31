package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setupTeams starts an API with an organization "acme", its projects "alphaprojectid", "betaprojectid" and
// "gammaprojectid", and a team "team-dev" with the user "user-id-2" and the project "alphaprojectid". The organization
// "other" has a team "team-other" without users or projects, "empty" has no teams, "noteams" does not have teams
// support, and in "nomembers" the user cannot manage members.
func setupTeams(t *testing.T) (*cmdFactory, *mockapi.Handler) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	myUserID := "my-user-id"
	orgID := "org-id-teams"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	makeTeamsOrg := func(id, name, label string, capabilities ...string) *mockapi.Org {
		org := makeOrg(id, name, label, myUserID, "flexible")
		org.Capabilities = capabilities
		org.Links["members"] = mockapi.HALLink{HREF: "/organizations/" + id + "/members"}
		return org
	}
	noMembers := makeTeamsOrg("org-id-nomembers", "nomembers", "No Members", "teams")
	delete(noMembers.Links, "members")
	apiHandler.SetOrgs([]*mockapi.Org{
		makeTeamsOrg(orgID, "acme", "ACME Inc.", "teams"),
		makeTeamsOrg("org-id-other", "other", "Other Inc.", "teams"),
		makeTeamsOrg("org-id-empty", "empty", "Empty Inc.", "teams"),
		makeTeamsOrg("org-id-noteams", "noteams", "No Teams"),
		noMembers,
	})
	apiHandler.SetOrgMembers(orgID, []*mockapi.OrgMember{
		{UserID: myUserID, Owner: true, Permissions: []string{}},
		{UserID: "user-id-2", Permissions: []string{}},
		{UserID: "user-id-3", Permissions: []string{}},
	})
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject("alphaprojectid", orgID, "test-vendor", "Alpha", "region-1"),
		makeProject("betaprojectid", orgID, "test-vendor", "Beta", "region-1"),
		makeProject("gammaprojectid", orgID, "test-vendor", "Gamma", "region-1"),
	})
	apiHandler.SetTeams([]*mockapi.Team{
		{
			ID:                 "team-dev",
			OrganizationID:     orgID,
			Label:              "Developers",
			ProjectPermissions: []string{"viewer", "development:contributor"},
		},
		{
			ID:                 "team-other",
			OrganizationID:     "org-id-other",
			Label:              "Others",
			ProjectPermissions: []string{"admin"},
		},
	})
	apiHandler.SetTeamMembers("team-dev", []*mockapi.TeamMember{{UserID: "user-id-2"}})
	apiHandler.SetTeamProjects("team-dev", []*mockapi.TeamProjectAccess{{
		OrganizationID: orgID,
		ProjectID:      "alphaprojectid",
		ProjectTitle:   "Alpha",
	}})

	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	return newCommandFactory(t, apiServer.URL, authServer.URL), apiHandler
}

func TestTeamCreate(t *testing.T) {
	t.Parallel()
	f, _ := setupTeams(t)

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

	// The API does not require unique labels.
	f.Run("team:create", "-o", "acme", "--label", "Developers", "-r", "admin", "--no-check-unique")
	assertTrimmed(t, "Admins\nDevelopers\nDevelopers\nOps", f.Run("teams", "-o", "acme",
		"--format", "plain", "--no-header", "--columns", "label"))
}

func TestTeamGetUpdateDelete(t *testing.T) {
	t.Parallel()
	f, _ := setupTeams(t)

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
	f, _ := setupTeams(t)

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
	f, _ := setupTeams(t)

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

func TestTeamSelection(t *testing.T) {
	t.Parallel()
	f, _ := setupTeams(t)

	cases := []struct {
		name       string
		args       []string
		wantErr    bool
		wantStderr string
	}{
		{
			name:       "team in another organization",
			args:       []string{"team:get", "-o", "other", "-t", "team-dev"},
			wantErr:    true,
			wantStderr: "The team Developers (team-dev) is not part of the selected organization, Other Inc. (other).",
		},
		{
			name:       "verbose",
			args:       []string{"team:get", "-o", "acme", "-t", "team-dev", "-v"},
			wantStderr: "Selected team: Developers (team-dev)",
		},
		{
			name:       "no team in non-interactive mode",
			args:       []string{"team:users", "-o", "acme"},
			wantErr:    true,
			wantStderr: "A --team is required (in non-interactive mode)",
		},
		{
			name:       "organization without teams support",
			args:       []string{"teams", "-o", "noteams"},
			wantErr:    true,
			wantStderr: "The organization No Teams (noteams) does not have teams support.",
		},
		{
			name:       "organization without access to members",
			args:       []string{"teams", "-o", "nomembers"},
			wantErr:    true,
			wantStderr: "You do not have permission to manage teams in the organization No Members (nomembers).",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, err := f.forSubtest(t).RunCombinedOutput(c.args...)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Contains(t, stderr, c.wantStderr)
		})
	}

	// Interactively, an organization's only team is selected by default.
	stdout, stderr, err := f.RunInteractive("", "team:get", "-o", "other", "-P", "id")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Selected team: Others (team-other) (by default)")
	assert.Equal(t, "team-other\n", stdout)

	_, stderr, err = f.RunInteractive("", "team:get", "-o", "empty")
	assert.Error(t, err)
	assert.Contains(t, stderr, "No teams were found in the organization Empty Inc. (empty)")

	// With more teams, one is chosen by number, in order of label.
	f.Run("team:create", "-o", "other", "--label", "Auditors", "-r", "admin")
	stdout, stderr, err = f.RunInteractive("1\n", "team:get", "-o", "other", "-P", "label")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Enter a number to choose a team:")
	assert.Equal(t, "Others\n", stdout)
}

func TestTeamSelectOrganization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		orgs       []*mockapi.Org
		wantStderr []string
	}{
		{
			name:       "no organizations",
			wantStderr: []string{"No organizations found.", "To create an organization, run: platform-test org:create"},
		},
		{
			name: "no organizations with teams",
			orgs: []*mockapi.Org{makeOrg("org-id-1", "acme", "ACME Inc.", "my-user-id", "flexible")},
			wantStderr: []string{
				"No organizations were found in which you can manage teams.",
				"To list organizations, run: platform-test organizations",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			authServer := mockapi.NewAuthServer(t)
			t.Cleanup(authServer.Close)
			apiHandler := mockapi.NewHandler(t)
			apiHandler.SetMyUser(&mockapi.User{ID: "my-user-id"})
			apiHandler.SetOrgs(c.orgs)
			apiServer := httptest.NewServer(apiHandler)
			t.Cleanup(apiServer.Close)
			f := newCommandFactory(t, apiServer.URL, authServer.URL)

			_, stderr, err := f.RunInteractive("", "teams")
			assert.Error(t, err)
			for _, s := range c.wantStderr {
				assert.Contains(t, stderr, s)
			}
		})
	}
}

func TestTeamList(t *testing.T) {
	t.Parallel()
	f, _ := setupTeams(t)
	f.Run("team:create", "-o", "other", "--label", "Auditors", "-r", "admin")

	// Human-readable output has a heading and hints.
	stdout, stderr, err := f.RunCombinedOutput("teams", "-o", "acme")
	require.NoError(t, err)
	assert.Contains(t, stdout, "team-dev")
	assert.Contains(t, stderr, "Teams in the organization ACME Inc. (acme):")
	assert.Contains(t, stderr, "To list team projects, run: platform-test team:projects")
	assert.NotContains(t, stderr, "teams --all")

	stdout, stderr, err = f.RunCombinedOutput("teams", "-o", "acme", "-p", "alphaprojectid")
	require.NoError(t, err)
	assert.Contains(t, stdout, "team-dev")
	assert.Contains(t, stderr, "Teams with access to the project Alpha (alphaprojectid):")
	assert.Contains(t, stderr, "To list all teams in the organization, run: platform-test teams --all")

	cases := []struct {
		name       string
		args       []string
		wantStdout string
	}{
		{
			name:       "all teams despite a project",
			args:       []string{"-o", "acme", "-p", "betaprojectid", "--all"},
			wantStdout: "Developers",
		},
		{name: "one page", args: []string{"-o", "other", "--count", "1"}, wantStdout: "Auditors"},
		{name: "all pages", args: []string{"-o", "other", "--count", "0"}, wantStdout: "Auditors\nOthers"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"teams", "--format", "plain", "--no-header", "--columns", "label"}, c.args...)
			assertTrimmed(t, c.wantStdout, f.forSubtest(t).Run(args...))
		})
	}

	_, stderr, err = f.RunCombinedOutput("teams", "-o", "empty")
	assert.Error(t, err)
	assert.Contains(t, stderr, "No teams found")
	assert.Contains(t, stderr, "To create a new team, run: platform-test team:create")
}

func TestTeamProjectAdd(t *testing.T) {
	t.Parallel()
	f, apiHandler := setupTeams(t)
	teamProjects := func(teamID string) string {
		return f.Run("team:projects", "-t", teamID, "--format", "plain", "--columns", "id", "--no-header")
	}

	cases := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "all with projects",
			args:       []string{"-t", "team-dev", "--all", "betaprojectid"},
			wantStderr: "The --all option cannot be used when project(s) are specified.",
		},
		{
			name:       "no projects in non-interactive mode",
			args:       []string{"-t", "team-dev"},
			wantStderr: "At least one project must be specified (in non-interactive mode).",
		},
		{
			name:       "all in an organization without projects",
			args:       []string{"-t", "team-other", "--all"},
			wantStderr: "No projects were found in the team's organization (org-id-other)",
		},
		{
			name:       "declined",
			args:       []string{"-t", "team-dev", "betaprojectid", "--no"},
			wantStderr: "Are you sure you want to add the project Beta (betaprojectid) to the team Developers (team-dev)?",
		},
		{
			name: "declined with multiple projects",
			args: []string{"-t", "team-dev", "betaprojectid", "gammaprojectid", "--no"},
			wantStderr: "Selected projects:\n • Beta (betaprojectid)\n • Gamma (gammaprojectid)\n\n" +
				"Are you sure you want to add these 2 projects to the team Developers (team-dev)?",
		},
		{
			name:       "project in another organization",
			args:       []string{"-t", "team-other", "alphaprojectid"},
			wantStderr: "validation failed: team_organization_id",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, err := f.forSubtest(t).RunCombinedOutput(append([]string{"team:project:add"}, c.args...)...)
			assert.Error(t, err)
			assert.Contains(t, stderr, c.wantStderr)
		})
	}
	assert.Empty(t, apiHandler.Requests("POST", "/teams/team-dev/project-access"))

	_, stderr, err := f.RunInteractive("", "team:project:add", "-t", "team-other")
	assert.Error(t, err)
	assert.Contains(t, stderr, "No projects were found in the team's organization (org-id-other)")

	// Projects are chosen by number, while they fit on the terminal.
	fi := f.forSubtest(t)
	fi.extraEnv = append(fi.extraEnv, "LINES=50")
	_, stderr, err = fi.RunInteractive("0\n\ny\n", "team:project:add", "-t", "team-dev")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Enter a number to select a project to add to the team:")
	assert.Contains(t, stderr, "Selected project(s):\n • Beta (betaprojectid)")
	assert.Contains(t, stderr, "Enter a number to add another project to the selection (or Enter to finish):")
	assert.Contains(t, stderr, "The project(s) were successfully added to the team Developers (team-dev).")
	assertTrimmed(t, "alphaprojectid\nbetaprojectid", teamProjects("team-dev"))

	// Otherwise, they are entered by ID.
	fi = f.forSubtest(t)
	fi.extraEnv = append(fi.extraEnv, "LINES=3")
	_, stderr, err = fi.RunInteractive("\nbetaprojectid\nnope\ngammaprojectid\ny\n",
		"team:project:add", "-t", "team-dev")
	require.NoError(t, err)
	assert.Contains(t, stderr, "A project ID is required")
	assert.Contains(t, stderr, "The team already has access to the project: betaprojectid")
	assert.Contains(t, stderr, "Project not found: nope")
	assert.Contains(t, stderr, "Selected project(s):\n • Gamma (gammaprojectid)")
	assertTrimmed(t, "alphaprojectid\nbetaprojectid\ngammaprojectid", teamProjects("team-dev"))

	_, stderr, err = f.RunInteractive("", "team:project:add", "-t", "team-dev")
	assert.Error(t, err)
	assert.Contains(t, stderr, "The team currently has access to the project(s): \n"+
		" • Alpha (alphaprojectid)\n • Beta (betaprojectid)\n • Gamma (gammaprojectid)\nNo new projects were found to add.")

	// All the organization's projects can be added at once.
	teamID := strings.TrimSpace(f.Run("team:create", "-o", "acme", "--label", "Ops", "-r", "admin", "--output-id"))
	_, stderr, err = f.RunCombinedOutput("team:project:add", "-o", "acme", "-t", teamID, "--all")
	require.NoError(t, err)
	assert.Contains(t, stderr,
		"Selected projects:\n • Alpha (alphaprojectid)\n • Beta (betaprojectid)\n • Gamma (gammaprojectid)")
	assertTrimmed(t, "alphaprojectid\nbetaprojectid\ngammaprojectid", teamProjects(teamID))
}

func TestTeamProjectDelete(t *testing.T) {
	t.Parallel()
	f, apiHandler := setupTeams(t)

	_, stderr, err := f.RunCombinedOutput("team:project:delete", "-t", "team-dev")
	assert.Error(t, err)
	assert.Contains(t, stderr, "A project ID must be specified (in non-interactive mode).")

	_, stderr, err = f.RunCombinedOutput("team:project:delete", "-t", "team-dev", "alphaprojectid", "--no")
	assert.Error(t, err)
	assert.Contains(t, stderr,
		"Are you sure you want to remove the project Alpha (alphaprojectid) from the team Developers (team-dev)?")
	assert.Empty(t, apiHandler.Requests("DELETE", "/teams/team-dev/project-access/*"))

	_, stderr, err = f.RunInteractive("0\ny\n", "team:project:delete", "-o", "acme", "-t", "team-dev")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Enter a number to choose a project to remove from the team:")
	assert.Contains(t, stderr,
		"The project Alpha (alphaprojectid) was successfully removed from the team Developers (team-dev).")
	assert.Len(t, apiHandler.Requests("DELETE", "/teams/team-dev/project-access/alphaprojectid"), 1)

	_, stderr, err = f.RunInteractive("", "team:project:delete", "-t", "team-dev")
	assert.Error(t, err)
	assert.Contains(t, stderr, "No projects were found on the team Developers (team-dev)")
}

func TestTeamProjectList(t *testing.T) {
	t.Parallel()
	f, _ := setupTeams(t)
	f.Run("team:project:add", "-t", "team-dev", "betaprojectid", "gammaprojectid")

	cases := []struct {
		name       string
		args       []string
		env        []string
		wantErr    bool
		wantStdout string
		wantStderr []string
	}{
		{
			name:       "invalid count",
			args:       []string{"--count", "abc"},
			wantErr:    true,
			wantStderr: []string{"The --count must be a number between 1 and 200, or 0 to disable pagination."},
		},
		{
			name:       "count too large",
			args:       []string{"--count", "201"},
			wantErr:    true,
			wantStderr: []string{"The --count must be a number between 1 and 200, or 0 to disable pagination."},
		},
		{
			name:       "one page",
			args:       []string{"--count", "1", "--format", "plain", "--no-header", "--columns", "title"},
			wantStdout: "Alpha\n",
		},
		{
			name:       "all pages",
			args:       []string{"--count", "0", "--format", "plain", "--no-header", "--columns", "title"},
			wantStdout: "Alpha\nBeta\nGamma\n",
		},
		{
			name:       "all pages without pagination",
			args:       []string{"--format", "plain", "--no-header", "--columns", "title"},
			env:        []string{EnvPrefix + "PAGINATION_ENABLED=0", EnvPrefix + "PAGINATION_COUNT=1"},
			wantStdout: "Alpha\nBeta\nGamma\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := f.forSubtest(t)
			fs.extraEnv = append(fs.extraEnv, c.env...)
			stdout, stderr, err := fs.RunCombinedOutput(append([]string{"team:projects", "-t", "team-dev"}, c.args...)...)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, c.wantStdout, stdout)
			for _, s := range c.wantStderr {
				assert.Contains(t, stderr, s)
			}
		})
	}

	// Human-readable output has a heading and hints.
	stdout, stderr, err := f.RunCombinedOutput("team:projects", "-o", "acme", "-t", "team-dev", "--count", "1")
	require.NoError(t, err)
	assert.Contains(t, stdout, "alphaprojectid")
	assert.NotContains(t, stdout, "betaprojectid")
	assert.Contains(t, stderr, "Projects in the team Developers (team-dev):")
	assert.Contains(t, stderr, "More projects are available")
	assert.Contains(t, stderr, "To add project(s) to the team, run: platform-test team:project:add [ids...]")

	_, stderr, err = f.RunCombinedOutput("team:projects", "-t", "team-other")
	require.NoError(t, err)
	assert.Contains(t, stderr, "No projects were found in the team Others (team-other).")
	assert.Contains(t, stderr, "To add project(s), run: platform-test team:project:add")
}

func TestTeamUserAddDelete(t *testing.T) {
	t.Parallel()
	f, apiHandler := setupTeams(t)

	cases := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "add without a user",
			args:       []string{"team:user:add", "-t", "team-dev"},
			wantStderr: "A user must be specified (in non-interactive mode).",
		},
		{
			name:       "add an unknown user ID",
			args:       []string{"team:user:add", "-o", "acme", "-t", "team-dev", "user-id-9"},
			wantStderr: "The user user-id-9 was not found in the organization ACME Inc. (acme).",
		},
		{
			name: "add declined",
			args: []string{"team:user:add", "-t", "team-dev", "user-id-3", "--no"},
			wantStderr: "Are you sure you want to add the user User user-id-3 (user-id-3@example.com) " +
				"to the team Developers (team-dev)?",
		},
		{
			name:       "delete without a user",
			args:       []string{"team:user:delete", "-t", "team-dev"},
			wantStderr: "A user must be specified (in non-interactive mode).",
		},
		{
			name: "delete an unknown email address",
			args: []string{"team:user:delete", "-o", "acme", "-t", "team-dev", "outsider@example.com"},
			wantStderr: "The user with email address outsider@example.com was not found in the organization " +
				"ACME Inc. (acme)",
		},
		{
			name: "delete declined",
			args: []string{"team:user:delete", "-t", "team-dev", "user-id-2", "--no"},
			wantStderr: "Are you sure you want to remove the user User user-id-2 (user-id-2@example.com) " +
				"from the team Developers (team-dev)?",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, err := f.forSubtest(t).RunCombinedOutput(c.args...)
			assert.Error(t, err)
			assert.Contains(t, stderr, c.wantStderr)
		})
	}
	assert.Empty(t, apiHandler.Requests("POST", "/teams/team-dev/members"))
	assert.Empty(t, apiHandler.Requests("DELETE", "/teams/team-dev/members/*"))

	// Interactively, a user is added by email address.
	_, stderr, err := f.RunInteractive("\nnot-an-email\nuser-id-3@example.com\ny\n",
		"team:user:add", "-o", "acme", "-t", "team-dev")
	require.NoError(t, err)
	assert.Contains(t, stderr, "An email address is required")
	assert.Contains(t, stderr, "Invalid email address: not-an-email")
	assert.Contains(t, stderr, "The user was successfully added to the team Developers (team-dev).")

	// And removed by number.
	_, stderr, err = f.RunInteractive("0\ny\n", "team:user:delete", "-o", "acme", "-t", "team-dev")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Enter a number to choose a user to remove:")
	assert.Contains(t, stderr,
		"The user User user-id-2 (user-id-2@example.com) was successfully removed from the team Developers (team-dev).")
	assertTrimmed(t, "user-id-3", f.Run("team:users", "-t", "team-dev",
		"--format", "plain", "--columns", "id", "--no-header"))

	_, stderr, err = f.RunInteractive("", "team:user:delete", "-t", "team-other")
	assert.Error(t, err)
	assert.Contains(t, stderr, "No team users were found.")
}

func TestTeamUserList(t *testing.T) {
	t.Parallel()
	f, _ := setupTeams(t)
	f.Run("team:user:add", "-t", "team-dev", "user-id-3")

	cases := []struct {
		name       string
		args       []string
		env        []string
		wantErr    bool
		wantStdout string
		wantStderr string
	}{
		{
			name:       "invalid count",
			args:       []string{"--count", "abc"},
			wantErr:    true,
			wantStderr: "The --count must be a number between 1 and 50, or 0 to disable pagination.",
		},
		{
			name:       "count too large",
			args:       []string{"--count", "51"},
			wantErr:    true,
			wantStderr: "The --count must be a number between 1 and 50, or 0 to disable pagination.",
		},
		{
			name:       "negative count",
			args:       []string{"--count=-1"},
			wantErr:    true,
			wantStderr: "The --count must be a number between 1 and 50, or 0 to disable pagination.",
		},
		{
			name:       "one page",
			args:       []string{"--count", "1"},
			wantStdout: "user-id-2\n",
		},
		{
			name:       "all pages",
			args:       []string{"--count", "0"},
			wantStdout: "user-id-2\nuser-id-3\n",
		},
		{
			name:       "all pages without pagination",
			env:        []string{EnvPrefix + "PAGINATION_ENABLED=0", EnvPrefix + "PAGINATION_COUNT=1"},
			wantStdout: "user-id-2\nuser-id-3\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := f.forSubtest(t)
			fs.extraEnv = append(fs.extraEnv, c.env...)
			args := append([]string{"team:users", "-t", "team-dev", "--format", "plain", "--no-header", "--columns", "id"},
				c.args...)
			stdout, stderr, err := fs.RunCombinedOutput(args...)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, c.wantStdout, stdout)
			assert.Contains(t, stderr, c.wantStderr)
		})
	}

	// Human-readable output has a heading and hints.
	stdout, stderr, err := f.RunCombinedOutput("team:users", "-o", "acme", "-t", "team-dev", "--count", "1")
	require.NoError(t, err)
	assert.Contains(t, stdout, "user-id-2@example.com")
	assert.NotContains(t, stdout, "user-id-3")
	assert.Contains(t, stderr, "Users in the team Developers (team-dev):")
	assert.Contains(t, stderr, "More users are available")
	assert.Contains(t, stderr, "To delete a user, run: platform-test team:user:delete [email]")

	_, stderr, err = f.RunCombinedOutput("team:users", "-t", "team-other")
	require.NoError(t, err)
	assert.Contains(t, stderr, "No users were found in the team Others (team-other).")
	assert.Contains(t, stderr, "To add a user, run: platform-test team:user:add [email]")
}
