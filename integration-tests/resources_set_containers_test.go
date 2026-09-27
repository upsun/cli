package tests

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setUpResourcesSetContainers configures a project whose next deployment has
// an app, two workers, three services and a task. The "app--mail" worker has
// autoscaling enabled, and "replica" supports horizontal scaling but has no
// instance_count.
func setUpResourcesSetContainers(apiHandler *mockapi.Handler) (projectID string) {
	disk := map[string]any{"minimum": map[string]any{"disk": 256}, "default": map[string]any{"disk": 512}}
	resources := func(size string, extra map[string]any) map[string]any {
		r := map[string]any{"profile_size": size}
		maps.Copy(r, extra)
		return r
	}
	container := func(typ, size string, extra map[string]any) map[string]any {
		c := map[string]any{"type": typ, "container_profile": "BALANCED", "resources": resources(size, nil)}
		maps.Copy(c, extra)
		return c
	}

	next := nextDeployment(map[string]any{
		"app": container("golang:1.23", "0.5", map[string]any{
			"resources": resources("0.5", disk), "instance_count": 1, "disk": 512,
		}),
	}, map[string]any{
		"BALANCED": map[string]any{
			"0.5": map[string]any{"cpu": 0.5, "memory": 1024, "cpu_type": "shared"},
			"1":   map[string]any{"cpu": 1, "memory": 2048, "cpu_type": "shared"},
			"2":   map[string]any{"cpu": 2, "memory": 4096, "cpu_type": "shared"},
		},
	})
	next["workers"] = map[string]any{
		"app--queue": container("golang:1.23", "0.5", map[string]any{"instance_count": 1}),
		"app--mail":  container("golang:1.23", "0.5", map[string]any{"instance_count": 2}),
	}
	next["services"] = map[string]any{
		"db": container("mariadb:11.4", "1", map[string]any{
			"resources": resources("1", disk), "instance_count": 1, "disk": 1024,
			"supports_horizontal_scaling": false,
		}),
		// No instance_count, which means 1.
		"replica": container("mariadb-replica:11.4", "0.5", map[string]any{
			"resources": resources("0.5", disk), "disk": 1024,
			"supports_horizontal_scaling": true,
		}),
		// No minimum disk, so no disk can be set.
		"cache": container("redis:7.2", "0.5", map[string]any{"instance_count": 1}),
	}
	next["tasks"] = map[string]any{
		"cleanup": map[string]any{"name": "cleanup", "type": "golang:1.23", "resources": resources("0.5", nil)},
	}
	next["project_info"] = map[string]any{
		"settings":     map[string]any{},
		"capabilities": map[string]any{"instance_limit": 4},
	}

	projectID, main := setUpResourcesProject(apiHandler, "org-id-1", next)
	main.SetAutoscalingSettings(map[string]any{
		"services": map[string]any{
			"app":       map[string]any{"enabled": false},
			"app--mail": map[string]any{"enabled": true},
		},
	})
	return projectID
}

// TestResourcesSet_Containers checks resources:set options across apps,
// workers, services and tasks: the updates sent, and the validation errors.
func TestResourcesSet_Containers(t *testing.T) {
	type patch map[string]map[string]map[string]any

	cases := []struct {
		name string
		args []string
		// wantPatch is the expected PATCH body, or nil if none may be sent.
		wantPatch patch
		wantErr   bool
		wantOut   []string
	}{
		{
			name:      "app count",
			args:      []string{"--count", "app:3"},
			wantPatch: patch{"webapps": {"app": {"instance_count": 3}}},
		},
		{
			name:      "worker count",
			args:      []string{"--count", "app--queue:2"},
			wantPatch: patch{"workers": {"app--queue": {"instance_count": 2}}},
		},
		{
			name:      "scalable service count",
			args:      []string{"--count", "replica:2"},
			wantPatch: patch{"services": {"replica": {"instance_count": 2}}},
			wantOut:   []string{"Summary of changes", "Instance count: increasing from 1 to 2"},
		},
		{
			name:      "count wildcard",
			args:      []string{"--count", "rep*:3"},
			wantPatch: patch{"services": {"replica": {"instance_count": 3}}},
		},
		{
			name:    "service without horizontal scaling",
			args:    []string{"--count", "db:2"},
			wantErr: true,
			wantOut: []string{"The service db does not support horizontal scaling."},
		},
		{
			name:    "task count",
			args:    []string{"--count", "cleanup:2"},
			wantErr: true,
			wantOut: []string{"The instance count of the task cleanup cannot be changed."},
		},
		{
			name:    "count with autoscaling",
			args:    []string{"--count", "app--mail:3"},
			wantErr: true,
			wantOut: []string{"cannot be changed when autoscaling is enabled"},
		},
		{
			name:    "count over the instance limit",
			args:    []string{"--count", "replica:5"},
			wantErr: true,
			wantOut: []string{"The instance count 5 exceeds the limit 4."},
		},
		{
			name:    "zero count",
			args:    []string{"--count", "app:0"},
			wantErr: true,
			wantOut: []string{"Invalid instance count 0"},
		},
		{
			name:    "several count errors",
			args:    []string{"--count", "db:2,cache:2"},
			wantErr: true,
			wantOut: []string{"Errors in --count values:", "The service db does not", "The service cache does not"},
		},
		{
			name:    "unchanged count",
			args:    []string{"--count", "app:1,replica:1"},
			wantOut: []string{"nothing to update"},
		},
		{
			name: "sizes and disks together",
			args: []string{"--size", "app:1,db:2,cleanup:1", "--disk", "db:2048"},
			wantPatch: patch{
				"webapps":  {"app": {"resources": map[string]any{"profile_size": "1"}}},
				"services": {"db": {"resources": map[string]any{"profile_size": "2"}, "disk": 2048}},
				"tasks":    {"cleanup": {"resources": map[string]any{"profile_size": "1"}}},
			},
		},
		{
			name:    "unknown size",
			args:    []string{"--size", "app:3"},
			wantErr: true,
			wantOut: []string{"Size 3 not found in container profile BALANCED"},
		},
		{
			name:    "disk on a worker",
			args:    []string{"--disk", "app--queue:512"},
			wantErr: true,
			wantOut: []string{"The worker app--queue does not support a persistent disk."},
		},
		{
			name:    "disk on a service without one",
			args:    []string{"--disk", "cache:512"},
			wantErr: true,
			wantOut: []string{"The service cache does not support a persistent disk."},
		},
		{
			name:      "app object storage",
			args:      []string{"--object-storage", "app:1024"},
			wantPatch: patch{"webapps": {"app": {"resources": map[string]any{"disk": map[string]any{"object": 1024}}}}},
		},
		{
			name:    "service object storage",
			args:    []string{"--object-storage", "db:1024"},
			wantErr: true,
			wantOut: []string{"Object storage is only available on apps; db is a service."},
		},
		{
			name:    "unknown container",
			args:    []string{"--count", "nope:2"},
			wantErr: true,
			wantOut: []string{"Container nope not found."},
		},
		{
			name:    "missing value",
			args:    []string{"--count", "app"},
			wantErr: true,
			wantOut: []string{`app is not valid; it must be in the format "name:value".`},
		},
		{
			name:    "filtered out by --service",
			args:    []string{"--service", "replica", "--size", "app:1"},
			wantErr: true,
			wantOut: []string{"Container app not found."},
		},
		{
			name:    "dry run",
			args:    []string{"--count", "replica:2", "--dry-run"},
			wantOut: []string{"Summary of changes", "Instance count"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			apiHandler := mockapi.NewHandler(t)
			projectID := setUpResourcesSetContainers(apiHandler)
			f := serveAPI(t, apiHandler)

			args := append([]string{"resources:set", "-p", projectID, "-e", "main", "--no-wait", "--yes"}, c.args...)
			stdout, stderr, err := f.RunCombinedOutput(args...)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
			}
			for _, s := range c.wantOut {
				assert.Contains(t, stderr, s)
			}

			if c.wantPatch == nil {
				assert.Empty(t, apiHandler.DeploymentPatches(projectID, "main"), "no deployment update should be sent")
				return
			}
			assertJSONEq(t, c.wantPatch, deploymentPatch(t, apiHandler, projectID))
		})
	}
}

// TestResourcesSet_ContainersInteractive checks the interactive form for
// services: the instance count is asked only for a scalable service, and
// accepting the defaults changes nothing.
func TestResourcesSet_ContainersInteractive(t *testing.T) {
	cases := []struct {
		name      string
		service   string
		input     string
		wantCount bool
		wantPatch map[string]any
	}{
		{name: "scalable service defaults", service: "replica", input: "\n\n\n", wantCount: true},
		{
			name: "scalable service new count", service: "replica", input: "\n3\n\ny\n", wantCount: true,
			wantPatch: map[string]any{"services": map[string]any{"replica": map[string]any{"instance_count": 3}}},
		},
		{name: "other service defaults", service: "db", input: "\n\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			apiHandler := mockapi.NewHandler(t)
			projectID := setUpResourcesSetContainers(apiHandler)
			f := serveAPI(t, apiHandler)

			stdout, stderr, err := f.RunInteractive(
				c.input,
				"resources:set", "-p", projectID, "-e", "main", "--no-wait", "--service", c.service,
			)
			require.NoError(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

			if c.wantCount {
				assert.Contains(t, stderr, "Enter the number of instances")
			} else {
				assert.NotContains(t, stderr, "Enter the number of instances")
			}

			if c.wantPatch == nil {
				assert.Contains(t, stderr, "nothing to update")
				assert.Empty(t, apiHandler.DeploymentPatches(projectID, "main"), "no deployment update should be sent")
				return
			}
			assertJSONEq(t, c.wantPatch, deploymentPatch(t, apiHandler, projectID))
		})
	}
}

// assertJSONEq asserts that two values are equal once encoded as JSON.
func assertJSONEq(t *testing.T, want, got any) {
	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantJSON), string(gotJSON))
}
