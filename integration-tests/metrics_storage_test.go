package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func mountpointMetrics(diskUsed, diskLimit, inodesUsed, inodesLimit float64) map[string]any {
	return map[string]any{
		"disk_used":    map[string]any{"avg": diskUsed},
		"disk_limit":   map[string]any{"max": diskLimit},
		"inodes_used":  map[string]any{"avg": inodesUsed},
		"inodes_limit": map[string]any{"max": inodesLimit},
	}
}

func setupMetricsTest(t *testing.T, withStorage bool) (f *cmdFactory, projectID string) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	projectID = mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	obsPath := "/projects/" + projectID + "/environments/main/observability"
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "php:8.4", Size: "AUTO"},
		},
		Services: map[string]mockapi.App{
			"db": {Name: "db", Type: "mariadb:11.4", Size: "AUTO"},
		},
		Workers: map[string]mockapi.Worker{},
		Routes:  map[string]any{},
		Links:   mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get(obsPath+"/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_links": mockapi.MakeHALLinks("resources_overview=" + apiServer.URL + obsPath + "/resources/overview"),
		})
	})
	apiHandler.Get(obsPath+"/resources/overview", func(w http.ResponseWriter, _ *http.Request) {
		limits := map[string]any{
			"cpu_used":     map[string]any{"avg": 0.1},
			"cpu_limit":    map[string]any{"max": 1.0},
			"memory_used":  map[string]any{"avg": 256.0},
			"memory_limit": map[string]any{"max": 1024.0},
		}
		withMounts := func(mounts map[string]any) map[string]any {
			m := map[string]any{"mountpoints": mounts}
			for k, v := range limits {
				m[k] = v
			}
			return m
		}
		appMounts := map[string]any{
			"/mnt": mountpointMetrics(100, 1000, 10, 1000),
			"/tmp": mountpointMetrics(500, 1000, 40, 1000),
		}
		dbMounts := map[string]any{
			"/mnt": mountpointMetrics(250, 1000, 5, 1000),
			"/tmp": mountpointMetrics(10, 1000, 1, 1000),
		}
		if withStorage {
			appMounts["storage"] = mountpointMetrics(920, 1000, 30, 100)
			// Storage with a partial set of inode metrics.
			dbMounts["storage"] = map[string]any{
				"disk_used":    map[string]any{"avg": 500},
				"disk_limit":   map[string]any{"max": 1000},
				"inodes_limit": map[string]any{"max": 100},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_grain": 60,
			"_from":  1790189400,
			"_to":    1790190000,
			"data": []any{map[string]any{
				"timestamp": 1790190000,
				"services": map[string]any{
					"app": withMounts(appMounts),
					"db":  withMounts(dbMounts),
				},
			}},
		})
	})

	return newCommandFactory(t, apiServer.URL, authServer.URL), projectID
}

func TestMetricsStorage(t *testing.T) {
	cases := []struct {
		name        string
		withStorage bool
		env         []string
		args        []string
		want        string
		notWant     string
	}{
		{
			name:        "all storage columns",
			withStorage: true,
			args: []string{"metrics:all", "-1", "--no-header", "--format", "plain",
				"-c", "service,disk_percent,storage_percent,storage_inodes_percent"},
			want: "app\t10.0%\t92.0%\t30.0%\ndb\t25.0%\t50.0%\t\n",
		},
		{
			name:        "all table shows storage when present",
			withStorage: true,
			args:        []string{"metrics:all", "-1"},
			want:        "Storage %",
		},
		{
			name:    "all table hides storage when absent",
			args:    []string{"metrics:all", "-1"},
			notWant: "Storage",
		},
		{
			name: "all csv always includes storage",
			args: []string{"metrics:all", "-1", "--format", "csv"},
			want: "/tmp inodes %,Storage %\n",
		},
		{
			name:        "disk-usage storage columns",
			withStorage: true,
			args: []string{"disk", "-1", "--no-header", "-B", "--format", "plain",
				"-c", "service,storage_used,storage_limit,storage_percent,storage_ipercent"},
			want: "app\t920\t1000\t92.0%\t30.0%\ndb\t500\t1000\t50.0%\t\n",
		},
		{
			name:        "disk-usage table shows storage when present",
			withStorage: true,
			args:        []string{"disk", "-1"},
			want:        "Storage used",
		},
		{
			name:        "disabled",
			withStorage: true,
			env:         []string{"TEST_CLI_API_METRICS_STORAGE=0"},
			args:        []string{"metrics:all", "-1", "--format", "csv"},
			notWant:     "Storage",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, projectID := setupMetricsTest(t, c.withStorage)
			f.extraEnv = c.env
			args := append([]string{}, c.args...)
			args = append(args, "-p", projectID, "-e", "main")
			stdout, stderr, err := f.RunCombinedOutput(args...)
			require.NoError(t, err, "stderr: %s", stderr)
			if c.want != "" {
				assert.Contains(t, stdout, c.want)
			}
			if c.notWant != "" {
				assert.NotContains(t, stdout, c.notWant)
			}
		})
	}
}

func TestMetricsStorageDisabledColumn(t *testing.T) {
	f, projectID := setupMetricsTest(t, true)
	f.extraEnv = []string{"TEST_CLI_API_METRICS_STORAGE=0"}

	_, stderr, err := f.RunCombinedOutput("disk", "-1", "-c", "storage_used", "-p", projectID, "-e", "main")
	assert.Error(t, err)
	assert.Contains(t, stderr, "Column not found: storage_used")
}
