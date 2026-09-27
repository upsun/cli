package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setupResourcesSetContainers serves a project whose next deployment has an
// app, two workers, three services and a task. The "app--mail" worker has
// autoscaling enabled, and "replica" supports horizontal scaling but has no
// instance_count. It returns the command factory, the project ID and a
// holder for the deployment PATCH body.
func setupResourcesSetContainers(t *testing.T) (f *cmdFactory, projectID string, patchBody *atomic.Value) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	myUserID := "my-user-id"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})

	orgID := "org-id-1"
	apiHandler.SetOrgs([]*mockapi.Org{{
		ID:           orgID,
		Type:         "flexible",
		Name:         "acme",
		Label:        "Acme",
		Owner:        myUserID,
		Capabilities: []string{},
		Links: mockapi.MakeHALLinks(
			"self=/organizations/"+url.PathEscape(orgID),
			"profile=/organizations/"+url.PathEscape(orgID)+"/profile",
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

	envPath := "/projects/" + projectID + "/environments/main"
	autoscalingPath := envPath + "/autoscaling"
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#autoscaling"] = mockapi.HALLink{HREF: autoscalingPath}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiHandler.Get("/projects/"+projectID+"/settings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sizing_api_enabled": true})
	})
	apiHandler.Get("/organizations/"+orgID+"/profile", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})
	apiHandler.Get(autoscalingPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": map[string]any{
				"app":       map[string]any{"enabled": false},
				"app--mail": map[string]any{"enabled": true},
			},
			"_links": mockapi.MakeHALLinks("self=" + autoscalingPath),
		})
	})

	disk := map[string]any{"minimum": map[string]any{"disk": 256}, "default": map[string]any{"disk": 512}}
	withSize := func(size string, extra map[string]any) map[string]any {
		r := map[string]any{"profile_size": size}
		for k, v := range extra {
			r[k] = v
		}
		return r
	}
	nextPath := envPath + "/deployments/next"
	apiHandler.Get(nextPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webapps": map[string]any{
				"app": map[string]any{
					"name":              "app",
					"type":              "golang:1.23",
					"container_profile": "BALANCED",
					"resources":         withSize("0.5", disk),
					"instance_count":    1,
					"disk":              512,
				},
			},
			"workers": map[string]any{
				"app--queue": map[string]any{
					"name":              "app--queue",
					"type":              "golang:1.23",
					"container_profile": "BALANCED",
					"resources":         withSize("0.5", nil),
					"instance_count":    1,
				},
				"app--mail": map[string]any{
					"name":              "app--mail",
					"type":              "golang:1.23",
					"container_profile": "BALANCED",
					"resources":         withSize("0.5", nil),
					"instance_count":    2,
				},
			},
			"services": map[string]any{
				"db": map[string]any{
					"type":                        "mariadb:11.4",
					"container_profile":           "BALANCED",
					"resources":                   withSize("1", disk),
					"instance_count":              1,
					"disk":                        1024,
					"supports_horizontal_scaling": false,
				},
				// No instance_count, which means 1.
				"replica": map[string]any{
					"type":                        "mariadb-replica:11.4",
					"container_profile":           "BALANCED",
					"resources":                   withSize("0.5", disk),
					"disk":                        1024,
					"supports_horizontal_scaling": true,
				},
				// No minimum disk, so no disk can be set.
				"cache": map[string]any{
					"type":              "redis:7.2",
					"container_profile": "BALANCED",
					"resources":         withSize("0.5", nil),
					"instance_count":    1,
				},
			},
			"tasks": map[string]any{
				"cleanup": map[string]any{
					"name":      "cleanup",
					"type":      "golang:1.23",
					"resources": withSize("0.5", nil),
				},
			},
			"routes": map[string]any{},
			"project_info": map[string]any{
				"settings":     map[string]any{},
				"capabilities": map[string]any{"instance_limit": 4},
			},
			"container_profiles": map[string]any{
				"BALANCED": map[string]any{
					"0.5": map[string]any{"cpu": 0.5, "memory": 1024, "cpu_type": "shared"},
					"1":   map[string]any{"cpu": 1, "memory": 2048, "cpu_type": "shared"},
					"2":   map[string]any{"cpu": 2, "memory": 4096, "cpu_type": "shared"},
				},
			},
			"_links": mockapi.MakeHALLinks("self="+nextPath, "#edit="+nextPath),
		})
	})

	patchBody = &atomic.Value{}
	apiHandler.Patch(nextPath, func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(b, &body))
		patchBody.Store(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_embedded": map[string]any{"activities": []any{}},
		})
	})

	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	return newCommandFactory(t, apiServer.URL, authServer.URL), projectID, patchBody
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
			f, projectID, patchBody := setupResourcesSetContainers(t)

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
				assert.Nil(t, patchBody.Load(), "no deployment update should be sent")
				return
			}
			want, err := json.Marshal(c.wantPatch)
			require.NoError(t, err)
			got, err := json.Marshal(patchBody.Load())
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(got))
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
			f, projectID, patchBody := setupResourcesSetContainers(t)

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
				assert.Nil(t, patchBody.Load(), "no deployment update should be sent")
				return
			}
			want, err := json.Marshal(c.wantPatch)
			require.NoError(t, err)
			got, err := json.Marshal(patchBody.Load())
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(got))
		})
	}
}
