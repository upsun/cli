package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestActivityWait checks that environment actions wait for the activities they create,
// and exit with an error if an activity fails.
func TestActivityWait(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		status       string
		link         string
		args         []string
		activityType string
	}{
		{"deploy", "active", "deploy", []string{"environment:deploy", "--strategy", "stopstart"}, "environment.deploy"},
		{"redeploy", "active", "redeploy", []string{"environment:redeploy"}, "environment.redeploy"},
		{"activate", "inactive", "activate", []string{"environment:activate"}, "environment.activate"},
		{"pause", "active", "pause", []string{"environment:pause"}, "environment.pause"},
		{"resume", "paused", "resume", []string{"environment:resume"}, "environment.resume"},
		{"backup", "active", "backup", []string{"backup:create"}, "environment.backup"},
	}
	for _, c := range cases {
		for _, result := range []string{mockapi.ActivityResultSuccess, mockapi.ActivityResultFailure} {
			t.Run(c.name+"_"+result, func(t *testing.T) {
				t.Parallel()
				authServer := mockapi.NewAuthServer(t)
				defer authServer.Close()

				apiHandler := mockapi.NewHandler(t)
				apiServer := httptest.NewServer(apiHandler)
				defer apiServer.Close()

				projectID := mockapi.ProjectID()
				envPath := "/projects/" + projectID + "/environments/main"
				apiHandler.SetProjects([]*mockapi.Project{{
					ID: projectID,
					Links: mockapi.MakeHALLinks(
						"self=/projects/"+projectID,
						"environments=/projects/"+projectID+"/environments",
					),
					DefaultBranch: "main",
					// Activation reads the project settings and capabilities.
					Settings: map[string]any{},
				}})
				apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
					_ = json.NewEncoder(w).Encode(map[string]any{})
				})
				main := makeEnv(projectID, "main", "production", c.status, nil)
				main.Links["#activities"] = mockapi.HALLink{HREF: envPath + "/activities"}
				href := envPath + "/" + c.link
				if c.link == "backup" {
					href = envPath + "/backups"
				}
				main.Links["#"+c.link] = mockapi.HALLink{HREF: href}
				apiHandler.SetEnvironments([]*mockapi.Environment{main})

				if c.link == "deploy" {
					// Deploying needs staged changes.
					apiHandler.SetProjectActivities(projectID, []*mockapi.Activity{{
						ID:           "staged1",
						Type:         "environment.variable.create",
						State:        "staged",
						Project:      projectID,
						Environments: []string{"main"},
						Description:  "Mock User created a variable",
						CreatedAt:    time.Now().Add(-time.Hour),
					}})
				}
				apiHandler.SetActivityResult(result)

				f := newCommandFactory(t, apiServer.URL, authServer.URL)
				args := slices.Concat(c.args, []string{"-p", projectID, "-e", "main", "--yes"})
				stdOut, stdErr, err := f.RunCombinedOutput(args...)
				if result == mockapi.ActivityResultSuccess {
					require.NoError(t, err, "stdout: %s\nstderr: %s", stdOut, stdErr)
					assert.Contains(t, stdErr, "The activity succeeded")
				} else {
					require.Error(t, err, "stdout: %s\nstderr: %s", stdOut, stdErr)
					assert.Contains(t, stdErr, "The activity failed")
				}
				assert.Contains(t, stdErr, "Mock log for "+c.activityType)

				activities := apiHandler.ProjectActivities(projectID)
				require.NotEmpty(t, activities)
				assert.Equal(t, c.activityType, activities[0].Type)
				assert.Equal(t, "complete", activities[0].State)
				assert.Equal(t, result, activities[0].Result)
			})
		}
	}
}

// TestActivityCancel checks activity:cancel, with an ID and by default.
func TestActivityCancel(t *testing.T) {
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
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#activities"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/activities"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	now := time.Now()
	activity := func(id, state string, created time.Time) *mockapi.Activity {
		return &mockapi.Activity{
			ID:           id,
			Type:         "environment.redeploy",
			State:        state,
			Project:      projectID,
			Environments: []string{"main"},
			Description:  "Mock User redeployed main",
			CreatedAt:    created,
			UpdatedAt:    created,
		}
	}
	apiHandler.SetProjectActivities(projectID, []*mockapi.Activity{
		activity("done", "complete", now.Add(-3*time.Minute)),
		activity("running", "in_progress", now.Add(-2*time.Minute)),
		activity("pending", "pending", now.Add(-time.Minute)),
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	state := func(id string) string {
		for _, a := range apiHandler.ProjectActivities(projectID) {
			if a.ID == id {
				return a.State
			}
		}
		return ""
	}

	_, stdErr, err := f.RunCombinedOutput("activity:cancel", "running", "-p", projectID)
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "The activity was successfully")
	assert.Equal(t, mockapi.ActivityStateCancelled, state("running"))
	assert.Equal(t, "pending", state("pending"))

	// The most recent cancellable activity is chosen by default.
	_, stdErr, err = f.RunCombinedOutput("activity:cancel", "-p", projectID, "-e", "main")
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "The activity was successfully")
	assert.Equal(t, mockapi.ActivityStateCancelled, state("pending"))

	_, stdErr, err = f.RunCombinedOutput("activity:cancel", "-p", projectID, "-e", "main")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "No cancellable activities found")

	// A complete activity has no #cancel link.
	_, stdErr, err = f.RunCombinedOutput("activity:cancel", "done", "-p", projectID)
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Operation not available: cancel")
	assert.Equal(t, "complete", state("done"))
}
