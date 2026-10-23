package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestIntegrationAddUpdateTypes checks the request bodies that integration:add and integration:update send for each
// integration type.
func TestIntegrationAddUpdateTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typ        string
		add        []string // If nil, the integration exists, as existing (or with no properties).
		existing   *mockapi.Integration
		wantAdd    string
		update     []string
		wantUpdate string
	}{
		{
			typ: "github",
			add: []string{"--repository", "org/repo", "--token", "gh-token",
				"--base-url", "https://github.example.com", "--build-pull-requests", "1",
				"--build-draft-pull-requests", "0", "--build-pull-requests-post-merge", "1",
				"--pull-requests-clone-parent-data", "0", "--fetch-branches", "1", "--prune-branches", "0",
				"--resources-init", "minimum"},
			wantAdd: `{"type": "github", "repository": "org/repo", "token": "gh-token",
				"base_url": "https://github.example.com", "build_pull_requests": true,
				"build_draft_pull_requests": false, "build_pull_requests_post_merge": true,
				"pull_requests_clone_parent_data": false, "fetch_branches": true, "prune_branches": false,
				"environment_init_resources": "minimum"}`,
			update:     []string{"--repository", "org/other"},
			wantUpdate: `{"repository": "org/other"}`,
		},
		{
			typ: "gitlab",
			add: []string{"--server-project", "group/repo", "--token", "gl-token",
				"--base-url", "https://gitlab.example.com", "--build-merge-requests", "1",
				"--build-wip-merge-requests", "0", "--merge-requests-clone-parent-data", "1", "--fetch-branches", "0"},
			// Without fetching branches, they are not pruned.
			wantAdd: `{"type": "gitlab", "project": "group/repo", "token": "gl-token",
				"base_url": "https://gitlab.example.com", "build_merge_requests": true,
				"build_wip_merge_requests": false, "merge_requests_clone_parent_data": true,
				"fetch_branches": false, "prune_branches": false, "environment_init_resources": "parent"}`,
			update:     []string{"--build-merge-requests", "0", "--resources-init", "default"},
			wantUpdate: `{"build_merge_requests": false, "environment_init_resources": "default"}`,
		},
		{
			// Adding a Bitbucket integration validates its credentials with bitbucket.org.
			typ:      "bitbucket",
			existing: &mockapi.Integration{BuildPullRequests: true},
			update:   []string{"--key", "bb-key", "--secret", "bb-secret", "--resync-pull-requests", "1"},
			wantUpdate: `{"app_credentials": {"key": "bb-key", "secret": "bb-secret"},
				"resync_pull_requests": true}`,
		},
		{
			typ: "bitbucket_server",
			add: []string{"--bitbucket-url", "https://bitbucket.example.com", "--username", "user",
				"--token", "bbs-token", "--repository", "proj/repo", "--build-pull-requests", "0"},
			// The repository is split into a project and a repository.
			wantAdd: `{"type": "bitbucket_server", "url": "https://bitbucket.example.com", "username": "user",
				"token": "bbs-token", "project": "proj", "repository": "repo", "build_pull_requests": false,
				"fetch_branches": true, "prune_branches": true, "environment_init_resources": "parent"}`,
			update:     []string{"--repository", "proj/other", "--bitbucket-url", "https://bitbucket2.example.com"},
			wantUpdate: `{"project": "proj", "repository": "other", "url": "https://bitbucket2.example.com"}`,
		},
		{
			typ: "webhook",
			add: []string{"--url", "https://hooks.example.com/a", "--shared-key", "jws-key",
				"--events", "environment.push,environment.branch", "--states", "complete,in_progress",
				"--environments", "main,dev-*", "--excluded-environments", "dev-skip"},
			wantAdd: `{"type": "webhook", "url": "https://hooks.example.com/a", "shared_key": "jws-key",
				"events": ["environment.push", "environment.branch"], "states": ["complete", "in_progress"],
				"environments": ["main", "dev-*"], "excluded_environments": ["dev-skip"]}`,
			update:     []string{"--excluded-environments", "dev-skip,dev-other"},
			wantUpdate: `{"excluded_environments": ["dev-skip", "dev-other"]}`,
		},
		{
			typ: "health.email",
			add: []string{"--from-address", "alerts@example.com", "--recipients", "a@example.com,#admins"},
			wantAdd: `{"type": "health.email", "from_address": "alerts@example.com",
				"recipients": ["a@example.com", "#admins"]}`,
			update:     []string{"--recipients", "#viewers"},
			wantUpdate: `{"recipients": ["#viewers"]}`,
		},
		{
			typ:        "health.pagerduty",
			add:        []string{"--routing-key", "pd-key"},
			wantAdd:    `{"type": "health.pagerduty", "routing_key": "pd-key"}`,
			update:     []string{"--routing-key", "pd-key-2"},
			wantUpdate: `{"routing_key": "pd-key-2"}`,
		},
		{
			typ:        "health.slack",
			add:        []string{"--token", "slack-token", "--channel", "#alerts"},
			wantAdd:    `{"type": "health.slack", "token": "slack-token", "channel": "#alerts"}`,
			update:     []string{"--channel", "#other"},
			wantUpdate: `{"channel": "#other"}`,
		},
		{
			typ:        "health.webhook",
			add:        []string{"--url", "https://hooks.example.com/health", "--shared-key", "jws-key"},
			wantAdd:    `{"type": "health.webhook", "url": "https://hooks.example.com/health", "shared_key": "jws-key"}`,
			update:     []string{"--url", "https://hooks.example.com/health2"},
			wantUpdate: `{"url": "https://hooks.example.com/health2"}`,
		},
		{
			typ: "script",
			add: []string{"--file", "script.js", "--events", "environment.push", "--environments", "main"},
			wantAdd: `{"type": "script", "script": "// Script.\n", "events": ["environment.push"],
				"states": ["complete"], "environments": ["main"]}`,
			update:     []string{"--states", "pending"},
			wantUpdate: `{"states": ["pending"]}`,
		},
		// Log forwarding integrations are updated, not added: integration:add sends a "redaction" property, which is
		// not in the OpenAPI spec.
		{
			typ: "httplog",
			update: []string{"--url", "https://logs.example.com", "--header", "Authorization: Bearer a, b",
				"--header", "X-Source: cli", "--verify-tls", "0", "--excluded-services", "db,cache"},
			wantUpdate: `{"url": "https://logs.example.com",
				"headers": {"Authorization": "Bearer a, b", "X-Source": "cli"}, "tls_verify": false,
				"excluded_services": ["db", "cache"]}`,
		},
		{
			typ:        "newrelic",
			update:     []string{"--url", "https://log-api.newrelic.com/log/v1", "--license-key", "nr-key"},
			wantUpdate: `{"url": "https://log-api.newrelic.com/log/v1", "license_key": "nr-key"}`,
		},
		{
			typ: "splunk",
			update: []string{"--url", "https://splunk.example.com", "--token", "splunk-token", "--index", "main",
				"--sourcetype", "upsun"},
			wantUpdate: `{"url": "https://splunk.example.com", "token": "splunk-token", "index": "main",
				"sourcetype": "upsun"}`,
		},
		{
			typ:        "sumologic",
			update:     []string{"--url", "https://sumo.example.com", "--category", "upsun"},
			wantUpdate: `{"url": "https://sumo.example.com", "category": "upsun"}`,
		},
		{
			typ: "syslog",
			update: []string{"--syslog-host", "syslog.example.com", "--syslog-port", "514", "--protocol", "tcp",
				"--facility", "3", "--message-format", "rfc3164", "--auth-mode", "structured_data",
				"--auth-token", "syslog-token"},
			// The port and facility are sent as integers.
			wantUpdate: `{"host": "syslog.example.com", "port": 514, "protocol": "tcp", "facility": 3,
				"message_format": "rfc3164", "auth_mode": "structured_data", "auth_token": "syslog-token"}`,
		},
		{
			typ:        "otlplog",
			update:     []string{"--url", "https://otlp.example.com/v1/logs", "--header", "Api-Key: otlp-key"},
			wantUpdate: `{"url": "https://otlp.example.com/v1/logs", "headers": {"Api-Key": "otlp-key"}}`,
		},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			t.Parallel()
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()

			apiHandler := mockapi.NewHandler(t)
			bodies := &requestBodies{}
			apiServer := httptest.NewServer(bodies.record("/integrations", apiHandler))
			defer apiServer.Close()

			projectID := mockapi.ProjectID()
			apiHandler.SetProjects([]*mockapi.Project{{
				ID: projectID,
				Links: mockapi.MakeHALLinks(
					"self=/projects/"+projectID,
					"environments=/projects/"+projectID+"/environments",
					"integrations=/projects/"+projectID+"/integrations",
				),
				Repository:    mockapi.ProjectRepository{URL: "git@git.example.com:" + projectID + ".git"},
				DefaultBranch: "main",
			}})
			apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})
			apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"integrations": map[string]any{"enabled": false}})
			})

			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			f.dir = t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(f.dir, "script.js"), []byte("// Script.\n"), 0o600))

			id := c.typ
			if c.add == nil {
				existing := c.existing
				if existing == nil {
					existing = &mockapi.Integration{}
				}
				path := "/projects/" + projectID + "/integrations/" + id
				existing.ID, existing.Type = id, c.typ
				existing.Links = mockapi.MakeHALLinks("self="+path, "#edit="+path)
				apiHandler.SetProjectIntegrations(projectID, []*mockapi.Integration{existing})
			} else {
				args := append([]string{"integration:add", "-p", projectID, "--type", c.typ}, c.add...)
				_, stderr, err := f.RunCombinedOutput(args...)
				require.NoError(t, err, stderr)
				integrations := apiHandler.Integrations(projectID)
				require.Len(t, integrations, 1)
				id = integrations[0].ID
				assert.Contains(t, stderr, "Created integration "+id+" (type: "+c.typ+")")
				assert.JSONEq(t, c.wantAdd, bodies.last(t), "add request body")
			}

			args := append([]string{"integration:update", "-p", projectID, id}, c.update...)
			_, stderr, err := f.RunCombinedOutput(args...)
			require.NoError(t, err, stderr)
			assert.Contains(t, stderr, "Integration "+id+" ("+c.typ+") updated")
			assert.JSONEq(t, c.wantUpdate, bodies.last(t), "update request body")
		})
	}
}

// requestBodies records the bodies of POST and PATCH requests.
type requestBodies struct {
	mu     sync.Mutex
	bodies []string
}

// record wraps a handler, recording requests to paths that contain pathPart.
func (r *requestBodies) record(pathPart string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if (req.Method == http.MethodPost || req.Method == http.MethodPatch) && strings.Contains(req.URL.Path, pathPart) {
			b, _ := io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(b))
			r.mu.Lock()
			r.bodies = append(r.bodies, string(b))
			r.mu.Unlock()
		}
		next.ServeHTTP(w, req)
	})
}

// last returns the last recorded body.
func (r *requestBodies) last(t *testing.T) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.bodies)
	return r.bodies[len(r.bodies)-1]
}
