package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setUpRedeploy sets up a project with a main environment that can be redeployed, serving the API through wrap.
func setUpRedeploy(
	t *testing.T, wrap func(apiHandler *mockapi.Handler, projectID string) http.Handler,
) (f *cmdFactory, projectID string) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)
	apiHandler := mockapi.NewHandler(t)
	projectID = mockapi.ProjectID()
	apiServer := httptest.NewServer(wrap(apiHandler, projectID))
	t.Cleanup(apiServer.Close)
	envPath := "/projects/" + projectID + "/environments/main"
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#activities"] = mockapi.HALLink{HREF: envPath + "/activities"}
	main.Links["#redeploy"] = mockapi.HALLink{HREF: envPath + "/redeploy"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})
	f = newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = []string{"COLUMNS=500"}
	return f, projectID
}

// The integration activities are waited on separately from the environment activity, and the summary reports
// their refreshed results.
func TestActivityMonitorIntegrationActivityResult(t *testing.T) {
	t.Parallel()
	f, projectID := setUpRedeploy(t, func(apiHandler *mockapi.Handler, projectID string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/environments/main/redeploy") {
				apiHandler.ServeHTTP(w, r)
				return
			}
			now := time.Now().Truncate(time.Second)
			activity := func(id, typ, state, result string) *mockapi.Activity {
				a := &mockapi.Activity{
					ID: id, Type: typ, State: state, Result: result, Project: projectID,
					Environments: []string{"main"}, Description: "Mock " + typ,
					CreatedAt: now, StartedAt: now, UpdatedAt: now,
				}
				if state == "complete" {
					a.CompletionPercent = 100
				}
				return a
			}
			// The activities are complete in the store, but in progress in the response.
			apiHandler.SetProjectActivities(projectID, []*mockapi.Activity{
				activity("act1", "environment.redeploy", "complete", "success"),
				activity("act2", "integration.webhook", "complete", "success"),
			})
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "OK", "code": 200,
				"_embedded": map[string]any{"activities": []*mockapi.Activity{
					activity("act1", "environment.redeploy", "in_progress", ""),
					activity("act2", "integration.webhook", "in_progress", ""),
				}},
			})
		})
	})
	_, stdErr, err := f.RunCombinedOutput("environment:redeploy", "-p", projectID, "-e", "main", "--yes")
	require.NoError(t, err, stdErr)
	assert.NotContains(t, stdErr, "unknown result")
	assert.Contains(t, stdErr, "2/2 activities succeeded")
}

// When the log stream ends before its seal, it is fetched again, and the replayed items are not printed twice.
func TestActivityMonitorLogReconnect(t *testing.T) {
	t.Parallel()
	var fetches atomic.Int32
	f, projectID := setUpRedeploy(t, func(apiHandler *mockapi.Handler, _ string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/log") {
				apiHandler.ServeHTTP(w, r)
				return
			}
			item := func(id, msg string) string {
				b, _ := json.Marshal(map[string]any{"_id": id, "data": map[string]any{
					"timestamp": time.Now().Format(time.RFC3339), "message": msg + "\n"}})
				return string(b) + "\n"
			}
			if fetches.Add(1) == 1 {
				fmt.Fprint(w, item("1", "first line"))
				return
			}
			fmt.Fprint(w, item("1", "first line")+item("2", "second line")+`{"seal":true}`+"\n")
		})
	})
	_, stdErr, err := f.RunCombinedOutput("environment:redeploy", "-p", projectID, "-e", "main", "--yes")
	require.NoError(t, err, stdErr)
	assert.Equal(t, 1, strings.Count(stdErr, "first line"))
	assert.Equal(t, 1, strings.Count(stdErr, "second line"))
}
