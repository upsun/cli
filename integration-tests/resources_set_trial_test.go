package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setUpResourcesSetOrg configures an org, project, environment and deployment,
// ready for a resources:set --size change from the current 0.5.
func setUpResourcesSetOrg(apiHandler *mockapi.Handler, orgID string) (projectID string) {
	myUserID := "my-user-id"
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiHandler.SetOrgs([]*mockapi.Org{{
		ID:           orgID,
		Type:         "flexible",
		Name:         "acme",
		Label:        "Acme",
		Owner:        myUserID,
		Capabilities: []string{},
		Links: mockapi.MakeHALLinks(
			"self=/organizations/" + url.PathEscape(orgID),
		),
	}})

	projectID = mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:           projectID,
		Organization: orgID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
	})

	apiHandler.Get("/projects/"+projectID+"/settings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sizing_api_enabled": true,
		})
	})

	nextDeploymentPath := "/projects/" + projectID + "/environments/main/deployments/next"
	apiHandler.Get(nextDeploymentPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webapps": map[string]any{
				"app": map[string]any{
					"name":              "app",
					"type":              "golang:1.23",
					"container_profile": "BALANCED",
					"resources": map[string]any{
						"profile_size": "0.5",
					},
					"instance_count": 1,
					"disk":           512,
				},
			},
			"services": map[string]any{},
			"workers":  map[string]any{},
			"routes":   map[string]any{},
			"project_info": map[string]any{
				"settings":     map[string]any{},
				"capabilities": map[string]any{},
			},
			"container_profiles": map[string]any{
				"BALANCED": map[string]any{
					"0.1": map[string]any{
						"cpu":      "0.1",
						"memory":   "64",
						"cpu_type": "shared",
					},
					"0.5": map[string]any{
						"cpu":      "0.5",
						"memory":   "128",
						"cpu_type": "shared",
					},
					"1": map[string]any{
						"cpu":      "1",
						"memory":   "256",
						"cpu_type": "shared",
					},
				},
			},
		})
	})

	return projectID
}

// setTrial configures the org's trial endpoint response. A nil trial means no
// trial (the endpoint returns {"trial": null}).
func setTrial(apiHandler *mockapi.Handler, orgID string, trial map[string]any) {
	apiHandler.Get("/organizations/"+orgID+"/trial", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"trial": trial,
			"_links": map[string]any{
				"self": map[string]any{"href": "/organizations/" + orgID + "/trial"},
			},
		})
	})
}

// activeTrial returns an active trial with the given resource limits, which
// may be nil (not capped) as well as numeric.
func activeTrial(cpu, memory, storage any) map[string]any {
	return map[string]any{
		"status": "active",
		"model":  "upsun_extended",
		"resource_limit": map[string]any{
			"projects":     1,
			"environments": 2,
			"cpu":          cpu,
			"memory":       memory,
			"storage":      storage,
		},
	}
}

// setUsage configures the org's usage endpoint response, and returns a
// pointer to a counter of requests to the endpoint.
func setUsage(apiHandler *mockapi.Handler, orgID string, cpu, memory, storage float64) *int {
	requests := new(int)
	apiHandler.Get("/organizations/"+orgID+"/usage", func(w http.ResponseWriter, _ *http.Request) {
		*requests++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"org":      map[string]any{},
			"projects": []any{},
			"totals": map[string]any{
				"cpu":      cpu,
				"memory":   memory,
				"storage":  storage,
				"projects": 1,
			},
		})
	})
	return requests
}

func runResourcesSet(
	t *testing.T, apiHandler *mockapi.Handler, projectID, size string,
) (stdout, stderr string, err error) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	return f.RunCombinedOutput(
		"resources:set",
		"-p", projectID,
		"-e", "main",
		"--size", size,
		"--dry-run",
		"--no-wait",
	)
}

// TestResourcesSet_NoTrial checks that resources:set succeeds for an
// organization without a trial, and no longer fetches the organization
// profile (whose endpoint returns 403 since the billing system migration).
func TestResourcesSet_NoTrial(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	orgID := "org-no-trial"
	projectID := setUpResourcesSetOrg(apiHandler, orgID)
	setTrial(apiHandler, orgID, nil)

	// The profile endpoint is gated for migrated organizations; the command
	// must not request it (the run fails if it does).
	apiHandler.Get("/organizations/"+orgID+"/profile", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":  "This organization profile has moved to the billing profile.",
			"status": 403,
			"detail": "Forbidden.",
		})
	})

	stdout, stderr, err := runResourcesSet(t, apiHandler, projectID, "app:0.1")

	assert.NotContains(t, stderr, "Permission denied")
	assert.NotContains(t, stderr, "403 Forbidden")
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stderr+stdout, "Summary of changes")
}

// TestResourcesSet_TrialLimitExceeded checks that the resource limits of an
// active trial are enforced.
func TestResourcesSet_TrialLimitExceeded(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	orgID := "org-trial-exceeded"
	projectID := setUpResourcesSetOrg(apiHandler, orgID)
	setTrial(apiHandler, orgID, activeTrial(0.5, 10, 10))
	// The CPU limit is already used up, so increasing the app size from 0.5
	// to 1 CPU must be refused.
	setUsage(apiHandler, orgID, 0.5, 0.125, 0.5)

	_, stderr, err := runResourcesSet(t, apiHandler, projectID, "app:1")

	assert.Error(t, err)
	assert.Contains(t, stderr, "trial CPU limit")
}

// TestResourcesSet_TrialWithinLimits checks that changes within an active
// trial's resource limits are allowed.
func TestResourcesSet_TrialWithinLimits(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	orgID := "org-trial-within"
	projectID := setUpResourcesSetOrg(apiHandler, orgID)
	setTrial(apiHandler, orgID, activeTrial(4.5, 12, 20))
	usageRequests := setUsage(apiHandler, orgID, 0.5, 0.125, 0.5)

	stdout, stderr, err := runResourcesSet(t, apiHandler, projectID, "app:1")

	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stderr+stdout, "Summary of changes")
	assert.Equal(t, 1, *usageRequests, "the limit check must have run")
}

// TestResourcesSet_TrialNullCaps checks that null resource limits are treated
// as not capped, while the other limits are still enforced.
func TestResourcesSet_TrialNullCaps(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	orgID := "org-trial-null-caps"
	projectID := setUpResourcesSetOrg(apiHandler, orgID)
	// CPU and storage are not capped; the memory cap is already used up.
	setTrial(apiHandler, orgID, activeTrial(nil, 10, nil))
	setUsage(apiHandler, orgID, 0.5, 10, 0.5)

	_, stderr, err := runResourcesSet(t, apiHandler, projectID, "app:1")

	assert.Error(t, err)
	assert.NotContains(t, stderr, "trial CPU limit")
	assert.Contains(t, stderr, "trial memory limit")
}

// TestResourcesSet_TrialsNotEnabled checks that an error from the trial
// endpoint is tolerated, e.g. for vendors without trials.
func TestResourcesSet_TrialsNotEnabled(t *testing.T) {
	apiHandler := mockapi.NewHandler(t)
	orgID := "org-trial-disabled"
	projectID := setUpResourcesSetOrg(apiHandler, orgID)

	apiHandler.Get("/organizations/"+orgID+"/trial", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":  "Trials are not enabled.",
			"status": 403,
			"detail": "Forbidden.",
		})
	})

	stdout, stderr, err := runResourcesSet(t, apiHandler, projectID, "app:0.1")

	require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stderr+stdout, "Summary of changes")
}
