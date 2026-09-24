package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestActivityLogRefresh checks that --refresh, which Symfony passes as a
// string, is accepted by activity:log for an in-progress activity.
func TestActivityLogRefresh(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

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
	})

	activityPath := "/projects/" + projectID + "/activities/act1"
	logPath := activityPath + "/log"

	// The activity is in progress until its log has been fetched.
	var logFetched atomic.Bool
	apiHandler.Get(activityPath, func(w http.ResponseWriter, _ *http.Request) {
		state, percent := "in_progress", 50
		if logFetched.Load() {
			state, percent = "complete", 100
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                 "act1",
			"type":               "environment.redeploy",
			"state":              state,
			"result":             "success",
			"completion_percent": percent,
			"project":            projectID,
			"environments":       []string{"main"},
			"description":        "Mock redeploy",
			"text":               "Mock redeploy",
			"payload":            map[string]any{},
			"created_at":         "2014-04-01T10:00:00Z",
			"updated_at":         "2014-04-01T10:00:00Z",
			"_links":             mockapi.MakeHALLinks("self="+activityPath, "log="+logPath),
		})
	})
	apiHandler.Get(logPath, func(w http.ResponseWriter, _ *http.Request) {
		logFetched.Store(true)
		_, _ = w.Write([]byte(`{"_id":"1","data":{"timestamp":"2014-04-01T10:00:01Z","message":"Mock log line\n"}}` +
			"\n" + `{"seal":true}` + "\n"))
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	cases := []struct {
		name    string
		refresh string
		wantErr string
	}{
		{"numeric refresh waits", "1", ""},
		{"zero refresh reads log", "0", ""},
		{"non-numeric refresh", "abc", "The --refresh value must be an integer."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logFetched.Store(false)
			stdOut, stdErr, err := f.RunCombinedOutput("activity:log", "act1", "-p", projectID, "--refresh", c.refresh)
			if c.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, stdErr, c.wantErr)
				return
			}
			require.NoError(t, err, "stderr: %s", stdErr)
			assert.Contains(t, stdOut, "Mock log line")
		})
	}
}
