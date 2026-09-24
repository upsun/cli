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
	// Timestamps are relative to the request time, as recent points are treated differently.
	var (
		mu   sync.Mutex
		now  time.Time
		data func() []map[string]any
	)
	ts := func(minutesAgo int) time.Time { return now.Add(-time.Duration(minutesAgo) * time.Minute) }
	point := func(minutesAgo int, services map[string]any) map[string]any {
		p := map[string]any{"timestamp": ts(minutesAgo).Unix()}
		if services != nil {
			p["services"] = services
		}
		return p
	}
	row := func(minutesAgo int, rest string) string {
		mu.Lock()
		defer mu.Unlock()
		return ts(minutesAgo).Format("2006-01-02T15:04:05+00:00") + "\t" + rest
	}

	// Modeled on the API: recent points lack services that have not reported yet,
	// and the in-progress point has no "services" key at all.
	data = func() []map[string]any {
		return []map[string]any{
			point(3, map[string]any{"app": cpu(0.1, 1), "db": cpu(0.1, 1), "router": cpu(0.01, 0.1)}),
			point(2, map[string]any{"app": cpu(0.2, 1), "db": cpu(0.3, 1), "router": cpu(0.02, 0.1)}),
			point(1, map[string]any{"db": cpu(0.4, 1)}),
			point(0, nil),
		}
	}
	setData := func(d func() []map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		data = d
	}
	apiHandler.Get(envPath+"/observability/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_links": mockapi.MakeHALLinks(
				"resources_overview=" + apiServer.URL + envPath + "/observability/resources/overview"),
		})
	})
	apiHandler.Get(envPath+"/observability/resources/overview", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		now = time.Now().UTC()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_grain": 60,
			"_from":  ts(10).Unix(),
			"_to":    now.Unix(),
			"data":   data(),
		})
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	latest := func() string {
		return f.Run("metrics:cpu", "-p", projectID, "-e", "main", "--latest", "--format", "tsv", "--no-header")
	}

	out := latest()
	assertTrimmed(t, row(2, "app\t0.2\t1\t20.0%")+"\n"+
		row(2, "db\t0.3\t1\t30.0%")+"\n"+
		row(2, "router\t0.02\t0.1\t20.0%"), out)

	out = f.Run("metrics:cpu", "-p", projectID, "-e", "main", "--format", "tsv")
	assert.Contains(t, out, row(1, "db\t0.4\t1\t40.0%"))

	// A service that stopped reporting before the recent points is ignored.
	setData(func() []map[string]any {
		return []map[string]any{
			point(4, map[string]any{"app": cpu(0.1, 1), "db": cpu(0.1, 1)}),
			point(3, map[string]any{"app": cpu(0.2, 1)}),
			point(2, map[string]any{"app": cpu(0.3, 1)}),
			point(1, map[string]any{"app": cpu(0.4, 1)}),
		}
	})
	out = latest()
	assertTrimmed(t, row(1, "app\t0.4\t1\t40.0%"), out)

	// Older points are not skipped.
	setData(func() []map[string]any {
		return []map[string]any{
			point(4, map[string]any{"app": cpu(0.1, 1), "db": cpu(0.1, 1)}),
			point(3, map[string]any{"app": cpu(0.2, 1)}),
		}
	})
	out = latest()
	assertTrimmed(t, row(3, "app\t0.2\t1\t20.0%"), out)
}
