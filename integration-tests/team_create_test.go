package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestTeamCreateNoLabel is a regression test for a TypeError raised by the
// legacy team:create command when --label was omitted in non-interactive mode.
func TestTeamCreateNoLabel(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "user-for-team-create-test"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	org := makeOrg("org-id-1", "acme", "ACME Inc.", myUserID, "flexible")
	org.Capabilities = []string{"teams"}
	org.Links = mockapi.MakeHALLinks(
		"self=/organizations/org-id-1",
		"members=/organizations/org-id-1/members",
	)
	apiHandler.SetOrgs([]*mockapi.Org{org})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stderr, err := f.RunCombinedOutput("team:create", "--org", "acme", "--no-check-unique")
	require.Error(t, err)
	assert.Contains(t, stderr, "The --label option is required in non-interactive mode.")
	assert.NotContains(t, stderr, "TypeError")
}

// TestTeamCreateEmptyRole checks that "production:" is rejected rather than
// completed to the first role, admin.
func TestTeamCreateEmptyRole(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "user-for-team-create-empty-role-test"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	org := makeOrg("org-id-1", "acme", "ACME Inc.", myUserID, "flexible")
	org.Capabilities = []string{"teams"}
	org.Links = mockapi.MakeHALLinks(
		"self=/organizations/org-id-1",
		"members=/organizations/org-id-1/members",
	)
	apiHandler.SetOrgs([]*mockapi.Org{org})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stderr, err := f.RunCombinedOutput("team:create", "--org", "acme", "--label", "Ops", "--no-check-unique",
		"-r", "viewer", "-r", "production:")
	assert.Error(t, err)
	assert.Contains(t, stderr, "A role cannot be empty.")
	assert.Empty(t, apiHandler.Requests("POST", "/teams"))
}

func TestTeamCreateInteractive(t *testing.T) {
	t.Parallel()
	f, apiHandler := setupTeams(t)

	cases := []struct {
		name       string
		stdin      string
		wantErr    bool
		wantStderr []string
		wantRoles  string
	}{
		{
			name:  "viewer with environment type roles",
			stdin: "\nViewers\nv\nv\n\nc\ny\n",
			wantStderr: []string{
				"The label cannot be empty",
				"The team's project role can be admin (a) or viewer (v).",
				"Project role (default: viewer) [a/v]:",
				"Role on type development (default: none) [a/c/v/n]:",
				"Created team Viewers",
			},
			wantRoles: "- viewer\n- 'production:viewer'\n- 'development:contributor'",
		},
		{
			name:       "admin",
			stdin:      "Admins\nadmin\ny\n",
			wantStderr: []string{"Created team Admins"},
			wantRoles:  "- admin",
		},
		{
			name:    "quit environment type roles",
			stdin:   "Quitters\n\nq\n",
			wantErr: true,
			wantStderr: []string{
				"At least one environment type role must be specified when the project role is viewer.",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := f.forSubtest(t).RunInteractive(c.stdin, "team:create", "-o", "acme", "--output-id")
			if c.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			for _, s := range c.wantStderr {
				assert.Contains(t, stderr, s)
			}
			if c.wantRoles != "" {
				assertTrimmed(t, c.wantRoles, f.forSubtest(t).Run("team:get", "-t", strings.TrimSpace(stdout),
					"-P", "project_permissions"))
			}
		})
	}

	// Update the team, keeping the defaults except for the production role.
	_, stderr, err := f.RunInteractive("\n\na\n\n\ny\n", "team:update", "-t", "team-dev")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Enter the team's label (default: Developers):")
	assert.Contains(t, stderr, "Role on type development (default: contributor) [a/c/v/n]:")
	assert.Contains(t, stderr, "Summary of changes:\nRole on environment type production: [none] -> admin\n")
	assertTrimmed(t, "- viewer\n- 'production:admin'\n- 'development:contributor'",
		f.Run("team:get", "-t", "team-dev", "-P", "project_permissions"))
	assert.Len(t, apiHandler.Requests("PATCH", "/teams/team-dev"), 1)
}

func TestTeamCreateRoleOptions(t *testing.T) {
	t.Parallel()
	f, apiHandler := setupTeams(t)

	cases := []struct {
		name       string
		args       []string
		wantStderr []string
	}{
		{
			name:       "invalid project role",
			args:       []string{"--label", "Ops", "-r", "x"},
			wantStderr: []string{"Invalid role: x"},
		},
		{
			name: "unknown environment type, declined",
			args: []string{"--label", "Ops", "-r", "admin", "-r", "foo:viewer", "--no"},
			wantStderr: []string{
				"No environment type IDs match: foo",
				"Are you sure you want to create a new team Ops?",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, err := f.forSubtest(t).RunCombinedOutput(append([]string{"team:create", "-o", "acme"}, c.args...)...)
			assert.Error(t, err)
			for _, s := range c.wantStderr {
				assert.Contains(t, stderr, s)
			}
		})
	}
	assert.Empty(t, apiHandler.Requests("POST", "/teams"))

	// Environment type roles alone.
	teamID := strings.TrimSpace(f.Run("team:create", "-o", "acme", "--label", "Ops", "-r", "production:viewer",
		"--output-id"))
	assertTrimmed(t, "- 'production:viewer'", f.Run("team:get", "-t", teamID, "-P", "project_permissions"))
}

func TestTeamUpdate(t *testing.T) {
	t.Parallel()
	f, apiHandler := setupTeams(t)
	f.Run("team:create", "-o", "acme", "--label", "Ops", "-r", "admin")

	_, stderr, err := f.RunCombinedOutput("team:update", "-t", "team-dev", "--label", "Engineers", "--no")
	assert.Error(t, err)
	assert.Contains(t, stderr, "Summary of changes:\nLabel: Developers -> Engineers\n")
	assert.Empty(t, apiHandler.Requests("PATCH", "/teams/team-dev"))

	_, stderr, err = f.RunCombinedOutput("team:update", "-t", "team-dev", "--label", "ops")
	assert.Error(t, err)
	assert.Contains(t, stderr, "exists in the organization with the same label: ops")

	_, stderr, err = f.RunCombinedOutput("team:update", "-o", "acme", "-t", "team-dev", "--label", "ops",
		"--no-check-unique", "-r", "viewer", "-r", "production:admin", "-r", "development:none")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Label: Developers -> ops\n"+
		"Role on environment type production: [none] -> admin\n"+
		"Role on environment type development: contributor -> [none]\n")
	assertTrimmed(t, "ops", f.Run("team:get", "-o", "acme", "-t", "team-dev", "-P", "label"))
	assertTrimmed(t, "- viewer\n- 'production:admin'", f.Run("team:get", "-t", "team-dev", "-P", "project_permissions"))

	_, stderr, err = f.RunCombinedOutput("team:delete", "-o", "acme", "-t", "team-dev", "--yes")
	require.NoError(t, err)
	assert.Contains(t, stderr, "The team ops (team-dev) was deleted.")
}
