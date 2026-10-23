package tests

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setUpActivityFilters configures a project with activities of several types, states and results. The mock API
// filters them by the type, state, result and starts_at query parameters, like the API.
func setUpActivityFilters(t *testing.T) (f *cmdFactory, apiHandler *mockapi.Handler, projectID string) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)
	apiHandler = mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	projectID = mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#activities"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/activities"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	start, _ := time.Parse(time.RFC3339, "2014-04-01T10:00:00Z")
	activity := func(id, typ, state, result string, minutesAgo int, envs ...string) *mockapi.Activity {
		created := start.Add(-time.Duration(minutesAgo) * time.Minute)
		percent := 0
		if state == "complete" {
			percent = 100
		}
		return &mockapi.Activity{
			CompletionPercent: percent,
			ID:                id,
			Type:              typ,
			State:             state,
			Result:            result,
			Project:           projectID,
			Environments:      append([]string{}, envs...),
			Description:       "Mock " + typ,
			Text:              "Mock " + typ,
			CreatedAt:         created,
			UpdatedAt:         created,
		}
	}
	apiHandler.SetProjectActivities(projectID, []*mockapi.Activity{
		activity("push1", "environment.push", "complete", "success", 1, "main"),
		activity("backup", "environment.backup", "complete", "failure", 2, "main"),
		activity("redeploy", "environment.redeploy", "in_progress", "", 3, "main"),
		activity("envvar", "environment.variable.create", "pending", "", 4, "main"),
		activity("projvar", "project.variable.create", "complete", "success", 5),
		activity("push2", "environment.push", "complete", "success", 6, "main"),
		activity("cache", "project.clear_build_cache", "in_progress", "", 7),
	})

	return newCommandFactory(t, apiServer.URL, authServer.URL), apiHandler, projectID
}

// TestActivityFilters checks the type, state, result and start filters of the activity commands, which select the
// activities to list, or the default activity to show.
func TestActivityFilters(t *testing.T) {
	t.Parallel()
	f, _, projectID := setUpActivityFilters(t)

	cases := []struct {
		name string
		args []string
		// wantOut is the expected stdout, or for activity:log, the activity ID.
		wantOut    string
		wantErr    bool
		wantStdErr string
	}{
		{name: "list type", args: []string{"activity:list", "--type", "push"}, wantOut: "push1\npush2"},
		{
			name:    "list type on all environments",
			args:    []string{"activity:list", "--type", "variable.create", "--all"},
			wantOut: "envvar\nprojvar",
		},
		{
			name:    "list exclude type",
			args:    []string{"activity:list", "--exclude-type", "push"},
			wantOut: "backup\nredeploy\nenvvar",
		},
		{
			name:    "list state",
			args:    []string{"activity:list", "--state", "in_progress,pending"},
			wantOut: "redeploy\nenvvar",
		},
		{name: "list incomplete", args: []string{"activity:list", "--incomplete"}, wantOut: "redeploy\nenvvar"},
		{name: "list result", args: []string{"activity:list", "--result", "failure"}, wantOut: "backup"},
		{
			name:    "list start",
			args:    []string{"activity:list", "--start", "2014-04-01T09:57:00Z"},
			wantOut: "redeploy\nenvvar\npush2",
		},
		{
			name:       "list conflicting types",
			args:       []string{"activity:list", "--type", "push", "--exclude-type", "push"},
			wantOut:    "push1\npush2",
			wantStdErr: "The --exclude-type and --type options conflict.",
		},
		{
			name:       "list unknown type",
			args:       []string{"activity:list", "--type", "nope"},
			wantErr:    true,
			wantStdErr: "Unrecognized activity type to include: nope",
		},
		{name: "get type", args: []string{"activity:get", "--type", "backup"}, wantOut: "backup"},
		{name: "get exclude type", args: []string{"activity:get", "-x", "push"}, wantOut: "backup"},
		{name: "get state", args: []string{"activity:get", "--state", "pending"}, wantOut: "envvar"},
		{name: "get result", args: []string{"activity:get", "--result", "failure"}, wantOut: "backup"},
		{name: "get incomplete", args: []string{"activity:get", "--incomplete"}, wantOut: "redeploy"},
		{
			name:    "get on all environments",
			args:    []string{"activity:get", "--all", "--type", "project.%"},
			wantOut: "projvar",
		},
		{name: "log type", args: []string{"activity:log", "--type", "backup"}, wantOut: "backup"},
		{name: "log exclude type", args: []string{"activity:log", "-x", "push"}, wantOut: "backup"},
		{name: "log state", args: []string{"activity:log", "--state", "complete", "-x", "push"}, wantOut: "backup"},
		{name: "log result", args: []string{"activity:log", "--result", "success"}, wantOut: "push1"},
		{
			name:    "log incomplete",
			args:    []string{"activity:log", "--incomplete", "--refresh", "0"},
			wantOut: "redeploy",
		},
		{
			name:    "log on all environments",
			args:    []string{"activity:log", "--all", "--type", "project.variable.create"},
			wantOut: "projvar",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append(slices.Clone(c.args), "-p", projectID)
			switch c.args[0] {
			case "activity:list":
				args = append(args, "-e", "main", "--format", "plain", "--columns", "id", "--no-header")
			case "activity:get":
				args = append(args, "-e", "main", "-P", "id")
			default:
				args = append(args, "-e", "main")
			}
			stdout, stderr, err := f.RunCombinedOutput(args...)
			if c.wantErr {
				assertExitCode(t, 1, err)
			} else {
				require.NoError(t, err, stderr)
			}
			if c.args[0] == "activity:log" {
				assert.Contains(t, stderr, "Activity ID: "+c.wantOut)
			} else {
				assertTrimmed(t, c.wantOut, stdout)
			}
			if c.wantStdErr != "" {
				assert.Contains(t, stderr, c.wantStdErr)
			}
		})
	}
}

// TestActivityLogTimestamps checks that activity:log --timestamps shows the time of each message.
func TestActivityLogTimestamps(t *testing.T) {
	t.Parallel()
	f, _, projectID := setUpActivityFilters(t)

	stdout, stderr, err := f.RunCombinedOutput("activity:log", "backup", "-p", projectID, "--timestamps")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "2014-04-01")
	assert.Contains(t, stdout, "Mock log for environment.backup")

	stdout, stderr, err = f.RunCombinedOutput("activity:log", "backup", "-p", projectID)
	require.NoError(t, err, stderr)
	assert.NotContains(t, stdout, "2014-04-01")
}

// TestActivityCancelFilters checks that the type filters of activity:cancel select the activity to cancel.
func TestActivityCancelFilters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "type", args: []string{"--type", "variable.create"}, want: "envvar"},
		{name: "exclude type", args: []string{"--exclude-type", "redeploy"}, want: "envvar"},
		{name: "all environments", args: []string{"--all", "--type", "clear_build_cache"}, want: "cache"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f, apiHandler, projectID := setUpActivityFilters(t)

			args := append([]string{"activity:cancel", "-p", projectID, "-e", "main"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			require.NoError(t, err, stderr)
			assert.Contains(t, stderr, "The activity was successfully")

			var canceled []string
			for _, a := range apiHandler.ProjectActivities(projectID) {
				if a.State == mockapi.ActivityStateCancelled {
					canceled = append(canceled, a.ID)
				}
			}
			assert.Equal(t, c.want, strings.Join(canceled, ","))
		})
	}
}
