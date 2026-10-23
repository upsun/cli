package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestResourcesGet is a regression test for "Worker not found: Array (in app:
// Array)" raised when running resources:get without --app or --worker. The
// command defines those options as VALUE_IS_ARRAY (filters), so an empty
// default ([]) was being cast to string and treated as a worker name in
// Selector::selectRemoteContainer.
func TestResourcesGet(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	projectID, _ := setUpResourcesProject(apiHandler, "org-id-1", nextDeployment(map[string]any{
		"app": map[string]any{
			"name":              "app",
			"type":              "golang:1.23",
			"container_profile": "BALANCED",
			"resources":         map[string]any{"profile_size": "0.1"},
			"instance_count":    1,
			"disk":              512,
		},
	}, map[string]any{
		"BALANCED": map[string]any{
			"0.1": map[string]any{"cpu": 0.1, "memory": 256, "cpu_type": "guaranteed"},
		},
	}))
	f := serveAPI(t, apiHandler)

	stdout, stderr, err := f.RunCombinedOutput("resources:get", "-p", projectID, "-e", "main")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.NotContains(t, stderr, "Array to string conversion")
	assert.NotContains(t, stderr, "Worker not found")
	assert.Contains(t, stdout, "app")
}

// TestResourcesGet_Filters checks the options that select the containers to show.
func TestResourcesGet_Filters(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	projectID := setUpResourcesSetContainers(apiHandler)
	f := serveAPI(t, apiHandler)

	cases := []struct {
		name       string
		args       []string
		wantOut    string
		wantErr    bool
		wantStdErr string
	}{
		{name: "no filter", wantOut: "app\napp--mail\napp--queue\ncache\ndb\nreplica\ncleanup"},
		{name: "service", args: []string{"--service", "db", "--service", "app"}, wantOut: "app\ndb"},
		{name: "service wildcard", args: []string{"-s", "app*"}, wantOut: "app\napp--mail\napp--queue"},
		{name: "app", args: []string{"--app", "app"}, wantOut: "app"},
		{name: "worker", args: []string{"--worker", "app--queue"}, wantOut: "app--queue"},
		{name: "task", args: []string{"--task", "cleanup"}, wantOut: "cleanup"},
		{name: "type", args: []string{"--type", "mariadb"}, wantOut: "db"},
		{name: "full type", args: []string{"--type", "mariadb-replica:11.4"}, wantOut: "replica"},
		{
			name:    "service and type",
			args:    []string{"--service", "app*", "--type", "golang"},
			wantOut: "app\napp--mail\napp--queue",
		},
		{name: "cpu type", args: []string{"--cpu-type", "guaranteed"}},
		{
			name:       "unknown worker",
			args:       []string{"--worker", "app"},
			wantErr:    true,
			wantStdErr: "No workers were found matching the name(s): app",
		},
		{
			name:       "unknown type",
			args:       []string{"--type", "postgresql"},
			wantErr:    true,
			wantStdErr: "No services were found matching the type(s): postgresql",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{
				"resources:get", "-p", projectID, "-e", "main", "--format", "plain", "--no-header", "--columns", "service",
			}, c.args...)
			stdout, stderr, err := f.RunCombinedOutput(args...)
			if c.wantErr {
				assertExitCode(t, 1, err)
			} else {
				require.NoError(t, err, stderr)
			}
			assertTrimmed(t, c.wantOut, stdout)
			assert.Contains(t, stderr, c.wantStdErr)
		})
	}
}
