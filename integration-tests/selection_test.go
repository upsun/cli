package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

const (
	selectionUserID = "my-user-id"
	// selectionOrgID is in the ULID format, which --org treats as an ID rather than a name.
	selectionOrgID      = "01HZY6X8A1B2C3D4E5F6G7H8J9"
	singleEnvProjectID  = "singleenvproject"
	selectionOtherOrgID = "org-other"
)

// setUpSelection serves two projects that the user can access: the layout project, which has the environments
// "main" (the default) and "staging", and a project with one environment, "prod". Each is in its own organization.
func setUpSelection(t *testing.T) *cmdFactory {
	t.Helper()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: selectionUserID})
	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg(selectionOrgID, "acme", "Acme", selectionUserID, "flexible"),
		makeOrg(selectionOtherOrgID, "other", "Other", "other-user-id", "flexible"),
	})
	project := func(id, org, title, defaultBranch string) *mockapi.Project {
		p := makeProject(id, org, "test-vendor", title, "region-1")
		p.DefaultBranch = defaultBranch
		p.Links = mockapi.MakeHALLinks("self=/projects/"+id, "environments=/projects/"+id+"/environments")
		return p
	}
	mainProject := project(layoutProjectID, selectionOrgID, "Main Project", "main")
	mainProject.Repository = mockapi.ProjectRepository{URL: layoutGitURL}
	apiHandler.SetProjects([]*mockapi.Project{
		mainProject,
		project(singleEnvProjectID, selectionOtherOrgID, "Single Env", "prod"),
	})
	apiHandler.SetUserGrants([]*mockapi.UserGrant{
		{ResourceID: selectionOrgID, ResourceType: "organization", OrganizationID: selectionOrgID, UserID: selectionUserID,
			Permissions: []string{"admin"}},
		{ResourceID: layoutProjectID, ResourceType: "project", OrganizationID: selectionOrgID, UserID: selectionUserID,
			Permissions: []string{"admin"}},
		{ResourceID: singleEnvProjectID, ResourceType: "project", OrganizationID: selectionOtherOrgID,
			UserID: selectionUserID, Permissions: []string{"viewer"}},
	})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(layoutProjectID, "main", "production", "active", nil),
		makeEnv(layoutProjectID, "staging", "development", "active", "main"),
		// The mock API identifies environments by ID alone, so this one is not called "main".
		makeEnv(singleEnvProjectID, "prod", "production", "active", nil),
	})
	f := serveAPI(t, apiHandler)
	// Wide enough not to wrap the error messages.
	f.extraEnv = append(f.extraEnv, "COLUMNS=500")
	return f
}

// TestProjectAndEnvironmentSelection checks how commands select a project and environment, outside a project
// directory.
func TestProjectAndEnvironmentSelection(t *testing.T) {
	t.Parallel()
	f := setUpSelection(t)

	t.Run("from the service environment variables", func(t *testing.T) {
		// The variables are set inside the platform's containers.
		f := *f
		f.t = t
		f.extraEnv = append(f.extraEnv[:len(f.extraEnv):len(f.extraEnv)],
			"PLATFORM_PROJECT="+layoutProjectID, "PLATFORM_BRANCH=staging")
		assert.Equal(t, "staging\n", f.Run("env:info", "id"))
	})

	t.Run("interactive project choice", func(t *testing.T) {
		// Projects are listed by title. The second has one environment, which is selected by default.
		stdOut, stdErr, err := f.RunInteractive("1\n", "env:info", "id")
		require.NoError(t, err, stdErr)
		assert.Equal(t, "prod\n", stdOut)
		assert.Contains(t, stdErr, "Main Project ("+layoutProjectID+")")
		assert.Contains(t, stdErr, "(by default)")
	})

	t.Run("interactive environment choice", func(t *testing.T) {
		// The project's default environment is the default choice.
		stdOut, stdErr, err := f.RunInteractive("\n", "env:info", "-p", layoutProjectID, "id")
		require.NoError(t, err, stdErr)
		assert.Equal(t, "main\n", stdOut)
		assert.Contains(t, stdErr, "Default: main")

		stdOut, stdErr, err = f.RunInteractive("1\n", "env:info", "-p", layoutProjectID, "id")
		require.NoError(t, err, stdErr)
		assert.Equal(t, "staging\n", stdOut)
	})

	cases := []struct {
		name    string
		args    []string
		wantErr []string
	}{
		{
			name: "unknown project",
			args: []string{"env:info", "-p", "missingproject", "-e", "main"},
			wantErr: []string{
				"Specified project not found: missingproject",
				"Your projects are:",
				layoutProjectID + " - Main Project",
			},
		},
		{
			name: "no environment, non-interactive",
			args: []string{"env:info", "-p", layoutProjectID},
			wantErr: []string{
				"No environment specified.",
				"Specify one using --environment (-e), or go to a project directory.",
			},
		},
		{
			name:    "unknown environment",
			args:    []string{"env:info", "-p", layoutProjectID, "-e", "missing"},
			wantErr: []string{"Specified environment not found: missing"},
		},
		{
			name:    "environment argument and option",
			args:    []string{"env:activate", "-p", layoutProjectID, "-e", "main", "staging"},
			wantErr: []string{"You cannot use both the <environment> argument and the --environment option"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stdErr, err := f.RunCombinedOutput(c.args...)
			assert.Error(t, err)
			for _, s := range c.wantErr {
				assert.Contains(t, stdErr, s)
			}
		})
	}
}

// TestOrganizationSelection checks how organization commands select an organization, by --org, by the project, or
// interactively.
func TestOrganizationSelection(t *testing.T) {
	t.Parallel()
	f := setUpSelection(t)

	assert.Equal(t, "acme\n", f.Run("org:info", "-o", selectionOrgID, "name"))

	// The project's organization is selected for --project, or for the project of the current directory.
	stdOut, stdErr, err := f.RunCombinedOutput("org:info", "-p", layoutProjectID, "name")
	require.NoError(t, err, stdErr)
	assert.Equal(t, "acme\n", stdOut)
	assert.Contains(t, stdErr, "Project organization: Acme")

	dir := repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)
	inProject := *f
	inProject.dir = dir
	assert.Equal(t, "acme\n", inProject.Run("org:info", "name"))

	// The organization the user owns is the default choice.
	stdOut, stdErr, err = f.RunInteractive("\n", "org:info", "name")
	require.NoError(t, err, stdErr)
	assert.Equal(t, "acme\n", stdOut)
	assert.Contains(t, stdErr, "(default)")

	stdOut, stdErr, err = f.RunInteractive("1\n", "org:info", "name")
	require.NoError(t, err, stdErr)
	assert.Equal(t, "other\n", stdOut)

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no organization, non-interactive",
			args:    []string{"org:info", "name"},
			wantErr: "An organization name or ID (--org) is required.",
		},
		{
			name:    "unknown organization",
			args:    []string{"org:info", "-o", "missing", "name"},
			wantErr: "Organization not found: missing",
		},
		{
			name:    "project in another organization",
			args:    []string{"org:info", "-p", layoutProjectID, "-o", "other", "name"},
			wantErr: "The project " + layoutProjectID + " is not part of the organization " + selectionOtherOrgID,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stdErr, err := f.RunCombinedOutput(c.args...)
			assert.Error(t, err)
			assert.Contains(t, stdErr, c.wantErr)
		})
	}
}
