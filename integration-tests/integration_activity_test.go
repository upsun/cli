package tests

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestIntegrationActivity(t *testing.T) {
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
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})

	path := "/projects/" + projectID + "/integrations/"
	apiHandler.SetProjectIntegrations(projectID, []*mockapi.Integration{
		{ID: "hook1", Type: "webhook", Links: mockapi.MakeHALLinks("self=" + path + "hook1")},
		{ID: "quiet", Type: "health.email", Links: mockapi.MakeHALLinks("self=" + path + "quiet")},
	})

	created, _ := time.Parse(time.RFC3339, "2024-06-01T10:00:00Z")
	activity := func(id, typ, state, result string, minutes int) *mockapi.Activity {
		a := &mockapi.Activity{
			ID:           id,
			Type:         typ,
			State:        state,
			Result:       result,
			Project:      projectID,
			Integration:  "hook1",
			Environments: []string{"main"},
			Description:  "Mock " + id,
			Text:         "Mock " + id,
			CreatedAt:    created.Add(time.Duration(minutes) * time.Minute),
			UpdatedAt:    created.Add(time.Duration(minutes) * time.Minute),
		}
		if state == "complete" {
			a.CompletionPercent = 100
			a.CompletedAt = a.UpdatedAt
		}
		return a
	}
	apiHandler.SetProjectActivities(projectID, []*mockapi.Activity{
		activity("act1", "integration.webhook", "complete", "success", 0),
		activity("act2", "integration.webhook", "complete", "failure", 1),
		activity("scr3", "integration.script", mockapi.ActivityStateCancelled, "failure", 2),
		// An activity of another kind, which isn't listed for the integration.
		{ID: "other", Type: "environment.push", State: "complete", Project: projectID, CreatedAt: created},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	listCases := []struct {
		name string
		args []string
		want string
	}{
		{"all", nil, "scr3\nact2\nact1"},
		{"limit", []string{"--limit", "2"}, "scr3\nact2"},
		{"type", []string{"--type", "webhook"}, "act2\nact1"},
		{"exclude type", []string{"-x", "integration.script"}, "act2\nact1"},
		{"state", []string{"--state", mockapi.ActivityStateCancelled}, "scr3"},
		{"result", []string{"--result", "success"}, "act1"},
		{"start", []string{"--start", "2024-06-01T10:01:30Z"}, "act2\nact1"},
	}
	for _, c := range listCases {
		t.Run("list "+c.name, func(t *testing.T) {
			args := append([]string{"integration:activity:list", "-p", projectID, "hook1",
				"--format", "plain", "--no-header", "--columns", "id"}, c.args...)
			assertTrimmed(t, c.want, f.Run(args...))
		})
	}

	stdout, stderr, err := f.RunCombinedOutput("integration:activities", "-p", projectID, "hook1",
		"--columns", "id,state,result")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Activities on the project "+projectID+", integration hook1 (webhook):")
	assert.Contains(t, stdout, "| act2 | complete  | failure |")

	_, stderr, err = f.RunCombinedOutput("integration:activity:list", "-p", projectID, "hook1", "--incomplete")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "No activities found")
	assert.Equal(t, []string{"in_progress", "pending"},
		lastRequest(t, apiHandler, "GET", "/projects/*/integrations/hook1/activities").Query["state"])

	_, stderr, err = f.RunCombinedOutput("integration:activity:list", "-p", projectID, "hook1", "--start", "not a date")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Invalid --start date: not a date")

	_, stderr, err = f.RunCombinedOutput("integration:activity:list", "-p", projectID, "quiet")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "No activities found")

	// Get the most recent activity, or one by its ID or a partial ID.
	assertTrimmed(t, "scr3", f.Run("integration:activity:get", "-p", projectID, "hook1", "-P", "id"))
	assertTrimmed(t, "failure", f.Run("integration:activity:get", "-p", projectID, "hook1", "act2", "-P", "result"))
	assertTrimmed(t, "integration.script",
		f.Run("integration:activity:get", "-p", projectID, "hook1", "scr", "-P", "type"))
	stdout = f.Run("integration:activity:get", "-p", projectID, "hook1", "act1", "--format", "csv")
	assert.Contains(t, stdout, "integration,hook1\n")
	assert.Contains(t, stdout, "state,complete\n")

	_, stderr, err = f.RunCombinedOutput("integration:activity:get", "-p", projectID, "quiet")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "No integration activities found")

	// Read logs.
	stdout, stderr, err = f.RunCombinedOutput("integration:activity:log", "-p", projectID, "hook1", "act2", "-t")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Integration ID: hook1\nActivity ID: act2\nType: integration.webhook\n")
	assertTrimmed(t, "[2024-06-01T10:01:00+00:00] Mock log for integration.webhook", stdout)

	stdout = f.Run("integration:activity:log", "-p", projectID, "hook1")
	assertTrimmed(t, "Mock log for integration.script", stdout)

	_, stderr, err = f.RunCombinedOutput("integration:activity:log", "-p", projectID, "quiet")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "No integration activities found")
}
