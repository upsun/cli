package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestActivityNullTimestamp checks whether the legacy PHP code crashes when an
// API activity is returned with null completed_at / updated_at / created_at /
// started_at fields. PHPStan level 8 flags strtotime() calls on those nullable
// properties in legacy/src/Model/Activity.php, and new DateTime() on
// $activity->created_at in legacy/src/Service/ActivityLoader.php.
//
// We bypass mockapi's typed Activity model (which would always emit valid
// timestamps) by wrapping the handler and serving raw JSON for the activities
// endpoints.
func TestActivityNullTimestamp(t *testing.T) {
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
	main.Links["#activities"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/activities"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	// Raw JSON for a single "complete" activity (completion_percent=100, so
	// isComplete() returns true) with completed_at, updated_at, created_at and
	// started_at all literal null. This is the worst case for the PHPStan-flagged
	// strtotime() and new DateTime() calls.
	activityJSON := `{
		"id": "actnull",
		"type": "environment.variable.create",
		"state": "complete",
		"result": "success",
		"completion_percent": 100,
		"completed_at": null,
		"started_at": null,
		"created_at": null,
		"updated_at": null,
		"project": "` + projectID + `",
		"environments": ["main"],
		"description": "Activity with null timestamps",
		"text": "Activity with null timestamps",
		"payload": {}
	}`
	listJSON := "[" + activityJSON + "]"

	listPath := "/projects/" + projectID + "/environments/main/activities"
	getPath := "/projects/" + projectID + "/activities/actnull"
	getEnvPath := "/projects/" + projectID + "/environments/main/activities/actnull"

	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case listPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(listJSON))
			return
		case getPath, getEnvPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(activityJSON))
			return
		}
		apiHandler.ServeHTTP(w, r)
	})

	apiServer := httptest.NewServer(wrapped)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// Drive activity:list. This exercises ActivityLoader::load() which calls
	// new DateTime($activity->created_at) on line 130 when paginating.
	stdout, stderr, err := f.RunCombinedOutput("activity:list", "-p", projectID, "-e", "main", "--format", "plain")
	t.Logf("activity:list err=%v\nstdout=%s\nstderr=%s", err, stdout, stderr)

	// Drive activity:get. This exercises Model\Activity::getDuration() which
	// calls strtotime() on completed_at / updated_at / created_at (lines 17, 24,
	// 26).
	stdout2, stderr2, err2 := f.RunCombinedOutput("activity:get", "-p", projectID, "-e", "main", "actnull")
	t.Logf("activity:get err=%v\nstdout=%s\nstderr=%s", err2, stdout2, stderr2)

	// Also explicitly request the duration property so getDuration() is invoked
	// even if the table path skips it.
	stdout3, stderr3, err3 := f.RunCombinedOutput(
		"activity:get", "-p", projectID, "-e", "main", "actnull", "-P", "duration",
	)
	t.Logf("activity:get -P duration err=%v\nstdout=%s\nstderr=%s", err3, stdout3, stderr3)

	// Surface TypeError / fatal errors loudly.
	for _, out := range []string{stderr, stderr2, stderr3} {
		if strings.Contains(out, "TypeError") ||
			strings.Contains(out, "must be of type string") ||
			strings.Contains(out, "Fatal error") ||
			strings.Contains(out, "Uncaught") {
			t.Fatalf("legacy CLI raised a PHP error on null timestamp:\n%s", out)
		}
	}

	// If we reach here without any error, the strtotime / new DateTime calls
	// either tolerated null input or the null was sanitized before reaching them.
	if err != nil {
		t.Fatalf("activity:list failed unexpectedly: %v\nstderr=%s", err, stderr)
	}
	if err2 != nil {
		t.Fatalf("activity:get failed unexpectedly: %v\nstderr=%s", err2, stderr2)
	}
	if err3 != nil {
		t.Fatalf("activity:get -P duration failed unexpectedly: %v\nstderr=%s", err3, stderr3)
	}
}
