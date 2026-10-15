package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentDelete(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
		makeEnv(projectID, "test-1", "development", "active", "main"),
		makeEnv(projectID, "test-2", "development", "active", "main"),
		makeEnv(projectID, "dev", "development", "active", "main"),
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	cases := []struct {
		name        string
		args        []string
		extraEnv    []string
		wantErr     bool
		wantStdErr  []string
		wantMissing []string
	}{
		{
			name:        "single wildcard argument",
			args:        []string{"test-*"},
			wantStdErr:  []string{"2 environments found by ID.", "Selected environments: test-1, test-2"},
			wantMissing: []string{"Specified environment not found"},
		},
		{
			name:        "wildcard option",
			args:        []string{"-e", "test-*"},
			wantStdErr:  []string{"2 environments found by ID.", "Selected environments: test-1, test-2"},
			wantMissing: []string{"Specified environment not found"},
		},
		{
			name:        "ignores an unknown branch variable",
			args:        []string{"--type", "development"},
			extraEnv:    []string{"PLATFORM_BRANCH=missing"},
			wantStdErr:  []string{"3 environments found matching type(s): development"},
			wantMissing: []string{"Specified environment not found"},
		},
		{
			name:        "missing environment",
			args:        []string{"missing"},
			wantErr:     true,
			wantStdErr:  []string{"Environment not found: missing", "0 environments found by ID."},
			wantMissing: []string{"Specified environment not found"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f.extraEnv = c.extraEnv
			args := append([]string{"environment:delete", "-p", projectID}, c.args...)
			// Decline any confirmation, so nothing is deleted.
			_, stdErr, err := f.RunInteractive("n\nn\n", args...)
			if c.wantErr {
				assert.Error(t, err)
			}
			for _, s := range c.wantStdErr {
				assert.Contains(t, stdErr, s)
			}
			for _, s := range c.wantMissing {
				assert.NotContains(t, stdErr, s)
			}
		})
	}
}

// TestEnvironmentDeleteConfirmed checks deactivating environments and deleting their branches.
func TestEnvironmentDeleteConfirmed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		args       []string
		stdin      string
		wantErr    bool
		wantStdErr []string
		wantGone   []string
		wantStatus map[string]string
	}{
		{
			name:       "active, with the branch",
			args:       []string{"test-1"},
			stdin:      "y\ny\n",
			wantStdErr: []string{"Deleting environment test-1", "Deleting inactive environment test-1", "git fetch --prune"},
			wantGone:   []string{"test-1"},
		},
		{
			name:       "active, keeping the branch",
			args:       []string{"test-1"},
			stdin:      "y\nn\n",
			wantStdErr: []string{"Deleting environment test-1"},
			wantStatus: map[string]string{"test-1": "inactive"},
		},
		{
			name:       "inactive",
			args:       []string{"--inactive"},
			stdin:      "y\n",
			wantStdErr: []string{"1 inactive environment found.", "Deleting inactive environment test-2"},
			wantGone:   []string{"test-2"},
			wantStatus: map[string]string{"test-1": "active"},
		},
		{
			name:       "merged",
			args:       []string{"--merged"},
			stdin:      "y\ny\n",
			wantStdErr: []string{"1 merged environment found.", "Deleting inactive environment merged"},
			wantGone:   []string{"merged"},
		},
		{
			name:       "exclude",
			args:       []string{"test-*", "--exclude", "test-1"},
			stdin:      "y\n",
			wantStdErr: []string{"1 environment excluded by ID.", "Selected environment: test-2"},
			wantGone:   []string{"test-2"},
			wantStatus: map[string]string{"test-1": "active"},
		},
		{
			name: "parent with children",
			args: []string{"dev"},
			wantStdErr: []string{
				"1 environment excluded as it is has child environment(s).",
				"You can skip this check using: --allow-delete-parent",
				"No environments to delete.",
			},
			wantStatus: map[string]string{"dev": "active"},
		},
		{
			name:       "allow deleting a parent",
			args:       []string{"dev", "--allow-delete-parent"},
			stdin:      "y\ny\n",
			wantStdErr: []string{"Deleting inactive environment dev"},
			wantGone:   []string{"dev"},
			wantStatus: map[string]string{"dev-child": "active"},
		},
		{
			name:       "already being deleted",
			args:       []string{"deleting"},
			wantStdErr: []string{"is already being deleted.", "No environments to delete."},
			wantStatus: map[string]string{"deleting": "deleting"},
		},
		{
			name:       "in progress",
			args:       []string{"busy"},
			wantErr:    true,
			wantStdErr: []string{"has in-progress activity, and therefore can't be deleted yet."},
			wantStatus: map[string]string{"busy": "dirty"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := setUpDeleteProject(t)

			args := append([]string{"environment:delete", "-p", layoutProjectID}, c.args...)
			_, stdErr, err := p.f.RunInteractive(c.stdin, args...)
			if c.wantErr {
				assertExitCode(t, 1, err)
			} else {
				require.NoError(t, err)
			}
			for _, s := range c.wantStdErr {
				assert.Contains(t, stdErr, s)
			}
			for _, id := range c.wantGone {
				assert.Nil(t, p.env(id), "environment %s should be deleted", id)
			}
			for id, status := range c.wantStatus {
				if e := p.env(id); assert.NotNil(t, e, "environment %s", id) {
					assert.Equal(t, status, e.Status, "status of %s", id)
				}
			}
			assert.NotNil(t, p.env("main"))
		})
	}
}

// setUpDeleteProject configures environments in a range of states, which can be deactivated.
func setUpDeleteProject(t *testing.T) *testProject {
	env := func(name, status string, parent any) *mockapi.Environment {
		e := makeEnv(layoutProjectID, name, "development", status, parent)
		e.AddOperations("deactivate")
		return e
	}
	merged := env("merged", "active", "main")
	merged.MergeInfo = map[string]any{"commits_ahead": 0, "parent_ref": "abc123"}
	return setUpProject(t,
		makeEnv(layoutProjectID, "main", "production", "active", nil),
		env("test-1", "active", "main"),
		env("test-2", "inactive", "main"),
		merged,
		env("dev", "active", "main"),
		env("dev-child", "active", "dev"),
		env("deleting", "deleting", "main"),
		env("busy", "dirty", "main"),
	)
}
