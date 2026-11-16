package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// objectStorageMetrics has no inodes limit, like the API.
var objectStorageMetrics = map[string]any{
	"disk_used":   map[string]any{"avg": 400},
	"disk_limit":  map[string]any{"max": 1000},
	"inodes_used": map[string]any{"avg": 25},
}

// filterServices applies the services[] query filter, like the API.
func filterServices(req *http.Request, services map[string]any) map[string]any {
	var names []string
	for k, v := range req.URL.Query() {
		if strings.HasPrefix(k, "services[") {
			names = append(names, v...)
		}
	}
	if len(names) == 0 {
		return services
	}
	filtered := make(map[string]any)
	for _, n := range names {
		if svc, ok := services[n]; ok {
			filtered[n] = svc
		}
	}
	return filtered
}

type metricsTestOptions struct {
	storage       bool                     // The app reports network storage.
	mounts        map[string]mockapi.Mount // The app's mounts.
	objectStorage map[string]any           // The app's object storage metrics, if any.
	objectQuota   int                      // The app's object storage quota (MB).
}

// setupMetricsTest serves metrics for an app, a worker, a service and the router. Apps and workers report their
// instance disk as /mnt. A service's storage is its /mnt.
func setupMetricsTest(t *testing.T, opts metricsTestOptions) (
	f *cmdFactory, apiHandler *mockapi.Handler, projectID string,
) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	apiHandler = mockapi.NewHandler(t)
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
	app := mockapi.App{Name: "app", Type: "php:8.4", Size: "AUTO", Mounts: opts.mounts}
	if opts.objectQuota > 0 {
		app.Resources = map[string]any{"disk": map[string]any{"object": opts.objectQuota}}
	}
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": app,
		},
		Services: map[string]mockapi.Service{
			"db": {Type: "mariadb:11.4", Size: "AUTO"},
		},
		Workers: map[string]mockapi.Worker{
			"app--queue": {App: mockapi.App{Name: "app--queue", Type: "php:8.4", Size: "AUTO"}},
		},
		Routes: map[string]any{},
		Links:  mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get(obsPath+"/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_links": mockapi.MakeHALLinks("resources_overview=" + apiServer.URL + obsPath + "/resources/overview"),
		})
	})
	apiHandler.Get(obsPath+"/resources/overview", func(w http.ResponseWriter, req *http.Request) {
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
		if opts.storage {
			appMounts["storage"] = mountpointMetrics(920, 1000, 30, 100)
		}
		if opts.objectStorage != nil {
			appMounts["object storage"] = opts.objectStorage
		}
		workerMounts := map[string]any{
			"/mnt": mountpointMetrics(740, 1000, 6, 1000),
			"/tmp": mountpointMetrics(20, 1000, 2, 1000),
		}
		dbMounts := map[string]any{
			"/mnt": mountpointMetrics(250, 1000, 5, 1000),
			"/tmp": mountpointMetrics(10, 1000, 1, 1000),
		}
		routerMounts := map[string]any{
			"/tmp": mountpointMetrics(30, 1000, 3, 1000),
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_grain": 60,
			"_from":  1790189400,
			"_to":    1790190000,
			"data": []any{map[string]any{
				"timestamp": 1790190000,
				"services": filterServices(req, map[string]any{
					"app":        withMounts(appMounts),
					"app--queue": withMounts(workerMounts),
					"db":         withMounts(dbMounts),
					"router":     withMounts(routerMounts),
				}),
			}},
		})
	})

	return newCommandFactory(t, apiServer.URL, authServer.URL), apiHandler, projectID
}

// deprecationWarning returns the warning for a deprecated disk column, and its storage and instance disk replacements.
func deprecationWarning(column, storage, instance string) string {
	return "The column " + column + " is deprecated: use " + storage + " (services) or " +
		instance + " (apps and workers)."
}

func TestMetricsDisks(t *testing.T) {
	t.Parallel()
	storageMount := map[string]mockapi.Mount{"/files": {Source: "storage", SourcePath: "files"}}
	instanceMount := map[string]mockapi.Mount{"/cache": {Source: "instance", SourcePath: "cache"}}
	localMount := map[string]mockapi.Mount{"/cache": {Source: "local", SourcePath: "cache"}}
	cases := []struct {
		name       string
		opts       metricsTestOptions
		args       []string
		want       string
		wantStderr []string
		notWant    string
	}{
		{
			name: "all by container type",
			opts: metricsTestOptions{storage: true},
			args: []string{"metrics:all", "-1", "--no-header", "--format", "plain", "-c",
				"service,storage_percent,storage_inodes_percent,instance_disk_percent,instance_inodes_percent,tmp_disk_percent"},
			want: "app\t92.0%\t30.0%\t10.0%\t1.0%\t50.0%\n" +
				"app--queue\t\t\t74.0%\t0.6%\t2.0%\n" +
				"db\t25.0%\t0.5%\t\t\t1.0%\n" +
				"router\t\t\t\t\t3.0%\n",
		},
		{
			name: "disk-usage by container type",
			opts: metricsTestOptions{storage: true},
			args: []string{"disk", "-1", "--no-header", "-B", "--format", "plain", "-c",
				"service,storage_used,storage_limit,storage_ipercent,instance_used,instance_limit,instance_ipercent"},
			want: "app\t920\t1000\t30.0%\t100\t1000\t1.0%\n" +
				"app--queue\t\t\t\t740\t1000\t0.6%\n" +
				"db\t250\t1000\t0.5%\t\t\t\n" +
				"router\t\t\t\t\t\t\n",
		},
		{
			name: "all deprecated columns",
			opts: metricsTestOptions{storage: true},
			args: []string{"metrics:all", "-1", "--no-header", "--format", "plain", "-c",
				"service,disk_used,disk_limit,disk_percent,inodes_used,inodes_limit,inodes_percent", "-B"},
			want: "app\t100\t1000\t10.0%\t10\t1000\t1.0%\n" +
				"app--queue\t740\t1000\t74.0%\t6\t1000\t0.6%\n" +
				"db\t250\t1000\t25.0%\t5\t1000\t0.5%\n" +
				"router\t\t\t\t\t\t\n",
			wantStderr: []string{
				deprecationWarning("disk_used", "storage_used", "instance_disk_used"),
				deprecationWarning("disk_limit", "storage_limit", "instance_disk_limit"),
				deprecationWarning("disk_percent", "storage_percent", "instance_disk_percent"),
				deprecationWarning("inodes_used", "storage_inodes_used", "instance_inodes_used"),
				deprecationWarning("inodes_limit", "storage_inodes_limit", "instance_inodes_limit"),
				deprecationWarning("inodes_percent", "storage_inodes_percent", "instance_inodes_percent"),
			},
		},
		{
			name: "all deprecated columns by wildcard",
			args: []string{"metrics:all", "-1", "--no-header", "--format", "plain", "-B", "-c", "service,disk_*"},
			want: "app\t100\t1000\t10.0%\n" +
				"app--queue\t740\t1000\t74.0%\n" +
				"db\t250\t1000\t25.0%\n" +
				"router\t\t\t\n",
			wantStderr: []string{
				deprecationWarning("disk_used", "storage_used", "instance_disk_used"),
				deprecationWarning("disk_limit", "storage_limit", "instance_disk_limit"),
				deprecationWarning("disk_percent", "storage_percent", "instance_disk_percent"),
			},
		},
		{
			name: "disk-usage deprecated columns",
			args: []string{"disk", "-1", "--no-header", "--format", "plain", "-B", "-c",
				"service,used,limit,percent,iused,ilimit,ipercent"},
			want: "app\t100\t1000\t10.0%\t10\t1000\t1.0%\n" +
				"app--queue\t740\t1000\t74.0%\t6\t1000\t0.6%\n" +
				"db\t250\t1000\t25.0%\t5\t1000\t0.5%\n" +
				"router\t\t\t\t\t\t\n",
			wantStderr: []string{
				deprecationWarning("used", "storage_used", "instance_used"),
				deprecationWarning("limit", "storage_limit", "instance_limit"),
				deprecationWarning("percent", "storage_percent", "instance_percent"),
				deprecationWarning("iused", "storage_iused", "instance_iused"),
				deprecationWarning("ilimit", "storage_ilimit", "instance_ilimit"),
				deprecationWarning("ipercent", "storage_ipercent", "instance_ipercent"),
			},
		},
		{
			name:    "all table hides instance disk without instance mounts",
			opts:    metricsTestOptions{mounts: storageMount},
			args:    []string{"metrics:all", "-1"},
			want:    "| Storage % | /tmp % |",
			notWant: "Instance",
		},
		{
			name: "all table shows instance disk with instance mounts",
			opts: metricsTestOptions{mounts: instanceMount},
			args: []string{"metrics:all", "-1"},
			want: "| Memory % | Storage % | Instance disk % | /tmp % |",
		},
		{
			name: "disk-usage table shows instance disk with local mounts",
			opts: metricsTestOptions{mounts: localMount},
			args: []string{"disk", "-1"},
			want: "| Instance disk used | Instance disk limit | Instance disk % | Instance disk inodes % |",
		},
		{
			name:    "all table ignores instance mounts of filtered-out apps",
			opts:    metricsTestOptions{mounts: instanceMount},
			args:    []string{"metrics:all", "-1", "-s", "db"},
			notWant: "Instance",
		},
		{
			name: "all csv always includes instance disk",
			args: []string{"metrics:all", "-1", "--format", "csv"},
			want: "Timestamp,Service,CPU %,Memory %,Storage %,Object storage %,Instance disk %,/tmp %\n",
		},
		{
			name: "disk-usage csv",
			args: []string{"disk", "-1", "--format", "csv"},
			want: "Timestamp,Service,Storage used,Storage limit,Storage %,Storage inodes %," +
				"Object storage used,Object storage limit,Object storage %," +
				"Instance disk used,Instance disk limit,Instance disk %,Instance disk inodes %,/tmp %\n",
		},
		{
			name: "all object storage columns",
			opts: metricsTestOptions{objectStorage: objectStorageMetrics},
			args: []string{"metrics:all", "-1", "--no-header", "-B", "--format", "plain", "-s", "app,db",
				"-c", "service,object_storage_used,object_storage_limit,object_storage_percent,object_storage_count"},
			want: "app\t400\t1000\t40.0%\t25\ndb\t\t\t\t\n",
		},
		{
			name: "all object storage without a quota",
			opts: metricsTestOptions{objectStorage: map[string]any{
				"disk_used":   map[string]any{"avg": 400},
				"inodes_used": map[string]any{"avg": 25},
			}},
			args: []string{"metrics:all", "-1", "--no-header", "-B", "--format", "plain", "-s", "app",
				"-c", "service,object_storage_used,object_storage_limit,object_storage_percent,object_storage_count"},
			want: "app\t400\t\t\t25\n",
		},
		{
			name: "all table shows object storage when present",
			opts: metricsTestOptions{objectStorage: objectStorageMetrics},
			args: []string{"metrics:all", "-1"},
			want: "| Storage % | Object storage % |",
		},
		{
			name:    "all table hides object storage when absent",
			args:    []string{"metrics:all", "-1"},
			notWant: "Object",
		},
		{
			name: "all table shows object storage when the app has a quota",
			opts: metricsTestOptions{objectQuota: 1024},
			args: []string{"metrics:all", "-1"},
			want: "Object storage %",
		},
		{
			name:    "all table ignores object storage quotas of filtered-out apps",
			opts:    metricsTestOptions{objectQuota: 1024},
			args:    []string{"metrics:all", "-1", "-s", "db"},
			notWant: "Object",
		},
		{
			name: "disk-usage object storage columns",
			opts: metricsTestOptions{objectStorage: objectStorageMetrics},
			args: []string{"disk", "-1", "--no-header", "-B", "--format", "plain", "-s", "app",
				"-c", "service,object_used,object_limit,object_percent,object_count"},
			want: "app\t400\t1000\t40.0%\t25\n",
		},
		{
			name: "disk-usage table shows object storage when present",
			opts: metricsTestOptions{objectStorage: objectStorageMetrics},
			args: []string{"disk", "-1"},
			want: "| Object storage used | Object storage limit | Object storage % |",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f, _, projectID := setupMetricsTest(t, c.opts)
			// The terminal is wide enough for headers not to wrap.
			f.extraEnv = []string{"COLUMNS=500"}
			args := append([]string{}, c.args...)
			args = append(args, "-p", projectID, "-e", "main")
			stdout, stderr, err := f.RunCombinedOutput(args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.NotContains(t, stderr, "Warning")
			if c.want != "" {
				assert.Contains(t, stdout, c.want)
			}
			if c.notWant != "" {
				assert.NotContains(t, stdout, c.notWant)
			}
			for _, w := range c.wantStderr {
				assert.Contains(t, stderr, w)
			}
			if c.wantStderr == nil {
				assert.NotContains(t, stderr, "DEPRECATED")
			}
		})
	}
}

// TestMetricsDeprecatedColumnsHidden checks that deprecated columns are not listed or matched by wildcards.
func TestMetricsDeprecatedColumnsHidden(t *testing.T) {
	t.Parallel()
	f, _, projectID := setupMetricsTest(t, metricsTestOptions{})

	help := f.Run("metrics:all", "--help")
	assert.Contains(t, help, "storage_percent")
	assert.NotRegexp(t, `\b(disk|inodes)_`, help)
	assert.NotRegexp(t, `[ ,]i?(used|limit|percent)[*,]`, f.Run("disk", "--help"))

	stdout, stderr, err := f.RunCombinedOutput("metrics:all", "-1", "--format", "csv", "-c", "*inodes_percent",
		"-p", projectID, "-e", "main")
	require.NoError(t, err, stderr)
	assert.Equal(t, "Storage inodes %,Instance disk inodes %,/tmp inodes %\n"+
		",1.0%,4.0%\n,0.6%,0.2%\n0.5%,,0.1%\n,,0.3%\n", stdout)
	assert.Contains(t, stderr, "The pattern *inodes_percent no longer matches the deprecated columns: inodes_percent.")
	assert.NotContains(t, stderr, "DEPRECATED")
}
