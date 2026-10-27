package tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestSSHTaskSelection checks how --task selects a running task container, from the environment's task activities.
func TestSSHTaskSelection(t *testing.T) {
	t.Parallel()
	p := setUpProject(t, sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Mounts: map[string]mockapi.Mount{}},
		},
	}))
	created := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	task := func(id, name, state string, age time.Duration) *mockapi.Activity {
		a := &mockapi.Activity{
			ID:           id,
			Type:         "environment.task",
			Parameters:   map[string]any{"task": name},
			State:        state,
			Project:      layoutProjectID,
			Environments: []string{"main"},
			CreatedAt:    created.Add(-age),
			UpdatedAt:    created.Add(-age),
		}
		if state == "complete" {
			a.Result = mockapi.ActivityResultSuccess
			a.CompletedAt = created.Add(-age + time.Minute)
		}
		return a
	}
	p.api.SetProjectActivities(layoutProjectID, []*mockapi.Activity{
		task("cleanup1aaaa", "cleanup", "in_progress", 0),
		task("cleanup0zzzz", "cleanup", "complete", time.Hour),
		task("migrate1bbbb", "migrate", "in_progress", time.Minute),
		task("migrate2cccc", "migrate", "pending", 0),
		task("backup1dddd", "backup", "complete", 2*time.Hour),
	})
	f := p.f
	// Wide enough not to wrap the error messages.
	f.extraEnv = append(f.extraEnv, "COLUMNS=500")
	sshURL := func(container string) string {
		return layoutProjectID + "-main--" + container + "@ssh.cli-tests.example.com"
	}
	ssh := func(args ...string) []string {
		return append([]string{"ssh", "-p", layoutProjectID, "-e", "main", "--pipe"}, args...)
	}

	// The container is named after the task and the first 8 characters of the running activity's ID.
	assert.Equal(t, sshURL("cleanup--task--cleanup1"), f.Run(ssh("--task", "cleanup")...))

	// Parallel runs are chosen by an activity ID or its prefix, or interactively.
	assert.Equal(t, sshURL("migrate--task--migrate1"), f.Run(ssh("--task", "migrate", "--activity", "migrate1")...))
	stdOut, stdErr, err := f.RunInteractive("1\n", ssh("--task", "migrate")...)
	require.NoError(t, err, stdErr)
	assert.Equal(t, sshURL("migrate--task--migrate1"), stdOut)
	assert.Contains(t, stdErr, `Enter a number to choose which run of "migrate" to connect to:`)

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name: "parallel runs, non-interactive",
			args: []string{"--task", "migrate"},
			wantErr: `Multiple instances of the task "migrate" are running. Use --activity <id> to choose one, ` +
				`or run interactively. Activity IDs: migrate2cccc, migrate1bbbb`,
		},
		{
			name: "ambiguous activity ID",
			args: []string{"--task", "migrate", "--activity", "migrate"},
			wantErr: `The activity ID "migrate" is ambiguous for task "migrate". ` +
				`Matching running activity IDs: migrate2cccc, migrate1bbbb`,
		},
		{
			name:    "unknown activity ID",
			args:    []string{"--task", "migrate", "--activity", "cleanup1"},
			wantErr: `No running instance of the task "migrate" matches the activity ID "cleanup1"`,
		},
		{
			name: "no running instance",
			args: []string{"--task", "backup"},
			wantErr: `The task "backup" has no running instance (last run: activity backup1dddd, finished ` +
				`2026-10-01T07:01:00Z). SSH is only possible while a task is running.`,
		},
		{
			name:    "never run",
			args:    []string{"--task", "report"},
			wantErr: `The task "report" has no running instance. SSH is only possible while a task is running.`,
		},
		{
			name:    "task and app",
			args:    []string{"--task", "cleanup", "--app", "app"},
			wantErr: "The --app option cannot be used together with --task.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			_, stdErr, err := f.RunCombinedOutput(ssh(c.args...)...)
			assert.Error(t, err)
			assert.Contains(t, stdErr, c.wantErr)
		})
	}
}
