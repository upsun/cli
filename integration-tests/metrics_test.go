package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestMetricsLatest(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
	}})

	envPath := "/projects/" + projectID + "/environments/main"
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#observability-pipeline"] = mockapi.HALLink{HREF: apiServer.URL + envPath + "/observability"}
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps:  map[string]mockapi.App{"app": {Name: "app", Type: "golang:1.23", Size: "AUTO"}},
		Services: map[string]mockapi.App{"db": {Name: "db", Type: "mariadb:11.4", Size: "AUTO"}},
		Workers:  map[string]mockapi.Worker{},
		Routes:   map[string]any{},
		Links:    mockapi.MakeHALLinks("self=" + envPath + "/deployment/current"),
	})
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	cpu := func(used, limit float64) map[string]any {
		return map[string]any{"cpu_used": map[string]any{"avg": used}, "cpu_limit": map[string]any{"max": limit}}
	}
	// Modeled on the API: recent points lack services that have not reported yet,
	// and the in-progress point has no "services" key at all.
	var mu sync.Mutex
	data := []map[string]any{
		{"timestamp": 1790190060, "services": map[string]any{
			"app": cpu(0.1, 1), "db": cpu(0.1, 1), "router": cpu(0.01, 0.1)}},
		{"timestamp": 1790190120, "services": map[string]any{
			"app": cpu(0.2, 1), "db": cpu(0.3, 1), "router": cpu(0.02, 0.1)}},
		{"timestamp": 1790190180, "services": map[string]any{
			"db": cpu(0.4, 1)}},
		{"timestamp": 1790190240},
	}
	apiHandler.Get(envPath+"/observability/resources/overview", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_grain": 60,
			"_from":  1790190000,
			"_to":    1790190300,
			"data":   data,
		})
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assertTrimmed(t, `
Timestamp	Service	Used	Limit	Used %
2026-09-23T19:02:00+00:00	app	0.2	1	20.0%
2026-09-23T19:02:00+00:00	db	0.3	1	30.0%
2026-09-23T19:02:00+00:00	router	0.02	0.1	20.0%
`, f.Run("metrics:cpu", "-p", projectID, "-e", "main", "--latest", "--format", "tsv"))

	assert.Contains(t, f.Run("metrics:cpu", "-p", projectID, "-e", "main", "--format", "tsv"),
		"2026-09-23T19:03:00+00:00\tdb\t0.4\t1\t40.0%")

	// A service that stopped reporting before the last settled point is ignored.
	mu.Lock()
	data = []map[string]any{
		{"timestamp": 1790190060, "services": map[string]any{"app": cpu(0.1, 1), "db": cpu(0.1, 1)}},
		{"timestamp": 1790190120, "services": map[string]any{"app": cpu(0.2, 1)}},
		{"timestamp": 1790190180, "services": map[string]any{"app": cpu(0.3, 1)}},
		{"timestamp": 1790190240, "services": map[string]any{"app": cpu(0.4, 1)}},
	}
	mu.Unlock()
	assertTrimmed(t, `
Timestamp	Service	Used	Limit	Used %
2026-09-23T19:04:00+00:00	app	0.4	1	40.0%
`, f.Run("metrics:cpu", "-p", projectID, "-e", "main", "--latest", "--format", "tsv"))
}
