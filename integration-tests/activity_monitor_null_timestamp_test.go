package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestActivityMonitorNullTimestamp drives `environment:redeploy` against a
// response whose embedded activities have null started_at and created_at.
// This exercises Platformsh\Cli\Service\ActivityMonitor::getStart() (line 677)
// and the strtotime() call in waitMultiple() (line 481), both of which feed
// $activity->created_at directly to strtotime() without a null guard. The same
// shape of bug was previously fixed in Model\Activity (commit 915a95af) but
// these two sites in ActivityMonitor were missed.
//
// To skip the `count(nonIntegrationActivities) === 1` short-circuit in
// waitMultiple() (which would route through waitAndLog() and getLogStream(),
// requiring more mocking), the redeploy response embeds two non-integration
// activities, so the multi-activity branch (lines 449-515) runs.
func TestActivityMonitorNullTimestamp(t *testing.T) {
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

	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#redeploy"] = mockapi.HALLink{
		HREF: "/projects/" + projectID + "/environments/main/redeploy",
	}
	main.Links["#activities"] = mockapi.HALLink{
		HREF: "/projects/" + projectID + "/environments/main/activities",
	}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	// Two complete non-integration activities with null timestamps. The pair
	// avoids the count(nonIntegration)===1 short-circuit and forces the
	// multi-activity progress-bar branch in waitMultiple(), which calls
	// getStart() (line 456) and strtotime($activity->created_at) (line 481).
	// completion_percent=100 lets the wait loop exit on the first iteration.
	activityJSON := func(id string) string {
		return `{
			"id": "` + id + `",
			"type": "environment.redeploy",
			"state": "complete",
			"result": "success",
			"completion_percent": 100,
			"completed_at": null,
			"started_at": null,
			"created_at": null,
			"updated_at": null,
			"project": "` + projectID + `",
			"environments": ["main"],
			"description": "Redeploy with null timestamps",
			"text": "Redeploy with null timestamps",
			"payload": {}
		}`
	}
	redeployResponse := `{
		"_embedded": {
			"activities": [` + activityJSON("actA") + `,` + activityJSON("actB") + `]
		}
	}`
	// Project-level activities endpoint, queried by waitMultiple() to refresh
	// activity state on each poll (line 495).
	projectActivitiesJSON := "[" + activityJSON("actA") + "," + activityJSON("actB") + "]"

	redeployPath := "/projects/" + projectID + "/environments/main/redeploy"
	projectActivitiesPath := "/projects/" + projectID + "/activities"

	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == redeployPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(redeployResponse))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == projectActivitiesPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(projectActivitiesJSON))
			return
		}
		apiHandler.ServeHTTP(w, r)
	})

	apiServer := httptest.NewServer(wrapped)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdout, stderr, err := f.RunCombinedOutput(
		"environment:redeploy", "-p", projectID, "-e", "main", "-y", "--wait",
	)
	t.Logf("environment:redeploy err=%v\nstdout=%s\nstderr=%s", err, stdout, stderr)

	// Surface TypeError / fatal errors loudly.
	if strings.Contains(stderr, "TypeError") ||
		strings.Contains(stderr, "must be of type string") ||
		strings.Contains(stderr, "Fatal error") ||
		strings.Contains(stderr, "Uncaught") {
		t.Fatalf("legacy CLI raised a PHP error on null timestamp:\n%s", stderr)
	}

	if err != nil {
		t.Fatalf("environment:redeploy failed unexpectedly: %v\nstderr=%s", err, stderr)
	}
}
