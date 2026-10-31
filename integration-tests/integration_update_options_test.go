package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setUpIntegrationProject sets up a project on which all integration types are available.
func setUpIntegrationProject(t *testing.T) (f *cmdFactory, apiHandler *mockapi.Handler, projectID string) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)
	apiHandler = mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	projectID = mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
			"integrations=/projects/"+projectID+"/integrations",
		),
		DefaultBranch: "main",
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})
	apiHandler.Get("/projects/"+projectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"integrations": map[string]any{"enabled": false}})
	})

	return newCommandFactory(t, apiServer.URL, authServer.URL), apiHandler, projectID
}

func TestIntegrationUpdateOptions(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID := setUpIntegrationProject(t)
	path := "/projects/" + projectID + "/integrations/"
	reset := func() {
		apiHandler.SetProjectIntegrations(projectID, []*mockapi.Integration{
			{ID: "pr-off", Type: "github", Repository: "org/repo", Links: mockapi.MakeHALLinks("self="+path+"pr-off", "#edit="+path+"pr-off")},
			{ID: "pr-on", Type: "github", Repository: "org/repo", BuildPullRequests: true, Links: mockapi.MakeHALLinks("self="+path+"pr-on", "#edit="+path+"pr-on")},
		})
	}

	cases := []struct {
		name      string
		args      []string
		wantPatch map[string]any
		wantErr   string
	}{
		{
			name:      "enable pull requests and drafts",
			args:      []string{"pr-off", "--build-pull-requests=true", "--build-draft-pull-requests=true"},
			wantPatch: map[string]any{"build_pull_requests": true, "build_draft_pull_requests": true},
		},
		{
			name:      "drafts when pull requests are enabled",
			args:      []string{"pr-on", "--build-draft-pull-requests=true"},
			wantPatch: map[string]any{"build_draft_pull_requests": true},
		},
		{
			name:    "drafts when pull requests are disabled",
			args:    []string{"pr-off", "--build-draft-pull-requests=true"},
			wantErr: "The option --build-draft-pull-requests can only be used if --build-pull-requests is true.",
		},
		{
			name:    "disable pull requests and enable drafts",
			args:    []string{"pr-on", "--build-pull-requests=false", "--build-draft-pull-requests=true"},
			wantErr: "The option --build-draft-pull-requests can only be used if --build-pull-requests is true.",
		},
		{
			name:    "option of another type",
			args:    []string{"pr-on", "--username", "bob"},
			wantErr: "The option --username cannot be used with the integration type github.",
		},
		{
			name:    "invalid boolean",
			args:    []string{"pr-on", "--build-pull-requests=maybe"},
			wantErr: "Invalid value for 'Build pull requests': maybe",
		},
		{
			name:      "fetch-branches false implies prune-branches false",
			args:      []string{"pr-on", "--fetch-branches=false"},
			wantPatch: map[string]any{"fetch_branches": false, "prune_branches": false},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			reset()
			before := len(apiHandler.Requests("PATCH", "/projects/*/integrations/*"))
			args := append([]string{"integration:update", "-p", projectID, "--no-wait"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			requests := apiHandler.Requests("PATCH", "/projects/*/integrations/*")
			if c.wantErr != "" {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, c.wantErr)
				assert.Len(t, requests, before)
				return
			}
			require.NoError(t, err, stderr)
			require.Len(t, requests, before+1)
			assert.Equal(t, c.wantPatch, jsonObject(t, requests[before].Body))
		})
	}

	reset()
	_, stderr, err := f.RunCombinedOutput("integration:update", "-p", projectID, "--no-wait", "pr-off",
		"--build-pull-requests=true", "--build-draft-pull-requests=true")
	require.NoError(t, err, stderr)
	updated := apiHandler.Integrations(projectID)[0]
	assert.True(t, updated.BuildPullRequests)
	assert.True(t, updated.BuildDraftPullRequests)
}

func TestIntegrationUpdateTypeOptions(t *testing.T) {
	t.Parallel()
	f, apiHandler, projectID := setUpIntegrationProject(t)
	path := "/projects/" + projectID + "/integrations/"
	links := func(id string) mockapi.HalLinks {
		return mockapi.MakeHALLinks("self="+path+id, "#edit="+path+id)
	}
	apiHandler.SetProjectIntegrations(projectID, []*mockapi.Integration{
		{ID: "gl", Type: "gitlab", BaseURL: "https://gitlab.example.com", Links: links("gl")},
		{ID: "hook", Type: "webhook", URL: "https://hooks.example.com", Events: []string{"*"}, Links: links("hook")},
		{ID: "mail", Type: "health.email", Links: links("mail")},
		{ID: "script", Type: "script", Links: links("script")},
		{ID: "gh", Type: "github", Repository: "org/repo", Links: links("gh")},
	})
	scriptFile := filepath.Join(t.TempDir(), "script.js")
	require.NoError(t, os.WriteFile(scriptFile, []byte("console.log('hi');\n"), 0o600))

	cases := []struct {
		name      string
		args      []string
		wantPatch map[string]any
		wantErr   string
	}{
		{
			name: "gitlab",
			args: []string{"gl", "--server-project", "group/repo", "--base-url", "https://git.example.com",
				"--build-merge-requests=true", "--build-wip-merge-requests=false", "--merge-requests-clone-parent-data=false"},
			wantPatch: map[string]any{
				"project": "group/repo", "base_url": "https://git.example.com", "build_merge_requests": true,
				"build_wip_merge_requests": false, "merge_requests_clone_parent_data": false,
			},
		},
		{name: "gitlab project without a namespace", args: []string{"gl", "--server-project", "repo"}, wantErr: "Invalid value for 'Project': repo"},
		{name: "invalid base URL", args: []string{"gl", "--base-url", "not a url"}, wantErr: "Invalid URL"},
		{
			name: "github build options",
			args: []string{"gh", "--build-pull-requests=true", "--build-pull-requests-post-merge=true", "--pull-requests-clone-parent-data=false", "--fetch-branches=true", "--prune-branches=false"},
			wantPatch: map[string]any{
				"build_pull_requests": true, "build_pull_requests_post_merge": true, "pull_requests_clone_parent_data": false,
				"fetch_branches": true, "prune_branches": false,
			},
		},
		{
			name:      "webhook",
			args:      []string{"hook", "--events", "environment.push,environment.branch", "--environments", "main", "--shared-key", "secret"},
			wantPatch: map[string]any{"events": []any{"environment.push", "environment.branch"}, "environments": []any{"main"}, "shared_key": "secret"},
		},
		{name: "email from address", args: []string{"mail", "--from-address", "alerts@example.com"}, wantPatch: map[string]any{"from_address": "alerts@example.com"}},
		{name: "invalid email from address", args: []string{"mail", "--from-address", "not-an-email"}, wantErr: "not-an-email"},
		{name: "script file", args: []string{"script", "--file", scriptFile}, wantPatch: map[string]any{"script": "console.log('hi');\n"}},
		{name: "missing script file", args: []string{"script", "--file", scriptFile + ".missing"}, wantErr: "File not found"},
		{name: "redaction of another type", args: []string{"hook", "--redaction=true"}, wantErr: "The option --redaction cannot be used with the integration type webhook."},
		{name: "type", args: []string{"hook", "--type", "slack", "--shared-key", "other"}, wantPatch: map[string]any{"shared_key": "other"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			before := len(apiHandler.Requests("PATCH", "/projects/*/integrations/*"))
			args := append([]string{"integration:update", "-p", projectID, "--no-wait"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			requests := apiHandler.Requests("PATCH", "/projects/*/integrations/*")
			if c.wantErr != "" {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, c.wantErr)
				assert.Len(t, requests, before)
				return
			}
			require.NoError(t, err, stderr)
			require.Len(t, requests, before+1)
			assert.Equal(t, c.wantPatch, jsonObject(t, requests[before].Body))
		})
	}
}
