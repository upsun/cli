package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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
	// Timestamps are relative to now, as recent points are treated differently.
	// The point 1 minute ago stays recent for the CLI while the test runs for under a minute.
	now := time.Now().UTC()
	ts := func(minutesAgo int) time.Time { return now.Add(-time.Duration(minutesAgo) * time.Minute) }
	point := func(minutesAgo int, services map[string]any) map[string]any {
		p := map[string]any{"timestamp": ts(minutesAgo).Unix()}
		if services != nil {
			p["services"] = services
		}
		return p
	}
	row := func(minutesAgo int, rest string) string {
		return ts(minutesAgo).Format("2006-01-02T15:04:05+00:00") + "\t" + rest
	}

	// Modeled on the API: recent points lack services that have not reported yet,
	// and the in-progress point has no "services" key at all.
	var mu sync.Mutex
	data := []map[string]any{
		point(3, map[string]any{"app": cpu(0.1, 1), "db": cpu(0.1, 1), "router": cpu(0.01, 0.1)}),
		point(2, map[string]any{"app": cpu(0.2, 1), "db": cpu(0.3, 1), "router": cpu(0.02, 0.1)}),
		point(1, map[string]any{"db": cpu(0.4, 1)}),
		point(0, nil),
	}
	setData := func(d []map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		data = d
	}
	apiHandler.Get(envPath+"/observability/resources/overview", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_grain": 60,
			"_from":  ts(10).Unix(),
			"_to":    now.Unix(),
			"data":   data,
		})
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	latest := func() string {
		return f.Run("metrics:cpu", "-p", projectID, "-e", "main", "--latest", "--format", "tsv", "--no-header")
	}

	assertTrimmed(t, row(2, "app\t0.2\t1\t20.0%")+"\n"+
		row(2, "db\t0.3\t1\t30.0%")+"\n"+
		row(2, "router\t0.02\t0.1\t20.0%"), latest())

	assert.Contains(t, f.Run("metrics:cpu", "-p", projectID, "-e", "main", "--format", "tsv"),
		row(1, "db\t0.4\t1\t40.0%"))

	// A service that stopped reporting before the recent points is ignored.
	setData([]map[string]any{
		point(4, map[string]any{"app": cpu(0.1, 1), "db": cpu(0.1, 1)}),
		point(3, map[string]any{"app": cpu(0.2, 1)}),
		point(2, map[string]any{"app": cpu(0.3, 1)}),
		point(1, map[string]any{"app": cpu(0.4, 1)}),
	})
	assertTrimmed(t, row(1, "app\t0.4\t1\t40.0%"), latest())

	// Older points are not skipped.
	setData([]map[string]any{
		point(61, map[string]any{"app": cpu(0.1, 1), "db": cpu(0.1, 1)}),
		point(60, map[string]any{"app": cpu(0.2, 1)}),
	})
	assertTrimmed(t, row(60, "app\t0.2\t1\t20.0%"), latest())
}
