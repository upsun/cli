package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

// errorLines returns the non-empty lines of an error output, trimmed of the padding of error blocks.
func errorLines(stderr string) []string {
	var lines []string
	for line := range strings.Lines(stderr) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// assertCleanAPIError checks that an API error is reported without internals or the command's usage.
func assertCleanAPIError(t *testing.T, stderr string) {
	t.Helper()
	for _, s := range []string{"resulted in", "Exception", ".php", "[-h|--help]", "[detail]", "[message]", "PHP "} {
		assert.NotContains(t, stderr, s)
	}
}

// TestAPIErrorOutput checks how an API error is reported: once, with the API's message, and without the usage.
func TestAPIErrorOutput(t *testing.T) {
	t.Parallel()
	p := setUpProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil))
	path := "/projects/" + layoutProjectID + "/variables"
	request := "POST " + p.f.apiURL + path

	cases := []struct {
		name     string
		status   int
		body     string
		wantCode int
		want     []string
	}{
		{
			name:     "problem",
			status:   http.StatusNotFound,
			body:     mockapi.Problem(http.StatusNotFound, "The project was not found."),
			wantCode: 255,
			want:     []string{"The API returned 404 Not Found for " + request, "The project was not found."},
		},
		{
			name:     "legacy shape",
			status:   http.StatusConflict,
			body:     `{"status":"error","code":409,"message":"Conflict","detail":"Variable already exists"}`,
			wantCode: 255,
			want:     []string{"The API returned 409 Conflict for " + request, "Variable already exists"},
		},
		{
			name:     "detail per property",
			status:   http.StatusBadRequest,
			body:     `{"status":"error","code":400,"message":"Bad Request","detail":{"name":"Invalid name"}}`,
			wantCode: 255,
			want:     []string{"The API returned 400 Bad Request for " + request, "name: Invalid name"},
		},
		{
			name:     "message repeating the status",
			status:   http.StatusInternalServerError,
			body:     `{"status":"error","code":500,"message":"Internal Server Error"}`,
			wantCode: 255,
			want:     []string{"The API returned 500 Internal Server Error for " + request},
		},
		{
			name:     "proxy error page",
			status:   http.StatusBadGateway,
			body:     "<html><body><h1>502 Bad Gateway</h1></body></html>",
			wantCode: 255,
			want:     []string{"The API returned 502 Bad Gateway for " + request},
		},
		{
			name:     "forbidden",
			status:   http.StatusForbidden,
			body:     mockapi.Problem(http.StatusForbidden, "You do not have access."),
			wantCode: 6,
			want:     []string{"Permission denied. Check your permissions on the project.", "You do not have access."},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p.api.FailNext("POST", path, c.status, c.body)
			_, stderr, err := p.f.forSubtest(t).RunCombinedOutput("variable:create", "-p", layoutProjectID,
				"-l", "project", "--name", "foo", "--value", "bar")
			assertExitCode(t, c.wantCode, err)
			assert.Equal(t, append([]string{"Creating variable foo on the project Test Project (abcdefghijkl)"},
				c.want...), errorLines(stderr))
			assertCleanAPIError(t, stderr)
		})
	}

	// Verbose output keeps the details.
	p.api.FailNext("POST", path, http.StatusConflict, mockapi.Problem(http.StatusConflict, "Variable already exists"))
	_, stderr, err := p.f.RunCombinedOutput("variable:create", "-v", "-p", layoutProjectID,
		"-l", "project", "--name", "foo", "--value", "bar")
	assertExitCode(t, 255, err)
	assert.Contains(t, stderr, "resulted in a `409 Conflict` response")
}

// TestWriteCommandAPIErrors checks how write commands report a failed write request.
func TestWriteCommandAPIErrors(t *testing.T) {
	t.Parallel()

	// Each setup returns a factory, the mock API, and a project ID.
	type setup func(t *testing.T) (*cmdFactory, *mockapi.Handler, string)
	project := func(t *testing.T) (*cmdFactory, *mockapi.Handler, string) {
		main := makeEnv(layoutProjectID, "main", "production", "active", nil)
		main.AddOperations("branch")
		main.Links["#backup"] = mockapi.HALLink{HREF: "/projects/" + layoutProjectID + "/environments/main/backups"}
		dev := makeEnv(layoutProjectID, "dev", "development", "active", "main")
		dev.AddOperations("deactivate")
		p := setUpProject(t, main, dev)
		p.api.SetProjectVariables(layoutProjectID, []*mockapi.Variable{{Name: "env:FOO", Value: "foo",
			Links: mockapi.MakeHALLinks("self=/projects/"+layoutProjectID+"/variables/env:FOO",
				"#edit=/projects/"+layoutProjectID+"/variables/env:FOO")}})
		return p.f, p.api, layoutProjectID
	}
	domains := func(t *testing.T) (*cmdFactory, *mockapi.Handler, string) {
		api, f, projectID := setUpDomains(t)
		return f, api, projectID
	}
	users := func(t *testing.T) (*cmdFactory, *mockapi.Handler, string) {
		return setupProjectUsers(t)
	}
	teams := func(t *testing.T) (*cmdFactory, *mockapi.Handler, string) {
		f, api := setupTeams(t)
		return f, api, ""
	}
	integrations := func(t *testing.T) (*cmdFactory, *mockapi.Handler, string) {
		p := setUpProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil))
		p.api.Get("/projects/"+layoutProjectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"integrations": map[string]any{
				"enabled": true,
				"config":  map[string]any{"webhook": map[string]any{"enabled": true}},
			}})
		})
		return p.f, p.api, layoutProjectID
	}

	orgUsers := func(t *testing.T) (*cmdFactory, *mockapi.Handler, string) {
		f, api, projectID := setupProjectUsers(t)
		org := makeOrg("org-id-1", "org-1", "Org 1", "my-user-id", "flexible")
		org.Links["members"] = mockapi.HALLink{HREF: "/organizations/org-id-1/members"}
		org.Links["invitations"] = mockapi.HALLink{HREF: "/organizations/org-id-1/invitations"}
		api.SetOrgs([]*mockapi.Org{org})
		api.SetOrgMembers("org-id-1", []*mockapi.OrgMember{{UserID: "my-user-id", Owner: true, Permissions: []string{}}})
		return f, api, projectID
	}

	type outcome struct {
		code int
		want string
	}
	// The PROJECT argument is replaced by the project ID.
	cases := []struct {
		name    string
		setup   setup
		args    []string
		method  string
		path    string
		success string // A message claiming success.
		reports bool   // Whether the command reports API errors itself, and exits with 1.
		handled map[int]outcome
	}{
		{
			name: "environment:branch", setup: project,
			args:   []string{"environment:branch", "-p", "PROJECT", "-e", "main", "feature"},
			method: "POST", path: "/projects/*/environments/main/branch", success: "branched",
		},
		{
			name: "environment:delete", setup: project,
			args:   []string{"environment:delete", "-p", "PROJECT", "dev"},
			method: "POST", path: "/projects/*/environments/dev/deactivate", success: "deactivated",
			reports: true,
		},
		{
			name: "variable:update", setup: project,
			args:   []string{"variable:update", "-p", "PROJECT", "-l", "project", "env:FOO", "--value", "bar"},
			method: "PATCH", path: "/projects/*/variables/env:FOO", success: "updated",
		},
		{
			name: "domain:add", setup: domains,
			args:   []string{"domain:add", "-p", "PROJECT", "-e", "main", "example.com"},
			method: "POST", path: "/projects/*/environments/main/domains", success: "added",
		},
		{
			name: "user:add", setup: users,
			args:   []string{"user:add", "-p", "PROJECT", "new@example.com", "-r", "viewer", "-r", "development:viewer"},
			method: "POST", path: "/projects/*/invitations", success: "invited",
			// The invitation is sent again, with "force".
			handled: map[int]outcome{http.StatusConflict: {0, "A new invitation has been sent to new@example.com"}},
		},
		{
			name: "team:create", setup: teams,
			args:   []string{"team:create", "-o", "acme", "--label", "Ops", "--no-check-unique"},
			method: "POST", path: "/teams", success: "Created",
			handled: map[int]outcome{http.StatusConflict: {1, "A team already exists with the same label: Ops"}},
		},
		{
			name: "team:update", setup: teams,
			args:   []string{"team:update", "-t", "team-dev", "--label", "Devs"},
			method: "PATCH", path: "/teams/team-dev", success: "updated",
		},
		{
			name: "organization:user:add", setup: orgUsers,
			args:   []string{"organization:user:add", "-o", "org-1", "new@example.com"},
			method: "POST", path: "/organizations/*/invitations", success: "invited",
			handled: map[int]outcome{http.StatusConflict: {1, "An invitation has already been sent to new@example.com"}},
		},
		{
			name: "project:create", setup: teams,
			args:   []string{"project:create", "--org", "acme", "--title", "T", "--region", "test-region"},
			method: "POST", path: "/organizations/*/subscriptions", success: "created",
		},
		{
			name: "backup:create", setup: project,
			args:   []string{"backup:create", "-p", "PROJECT", "-e", "main"},
			method: "POST", path: "/projects/*/environments/main/backups", success: "Created",
		},
		{
			name: "integration:add", setup: integrations,
			args:   []string{"integration:add", "-p", "PROJECT", "--type", "webhook", "--url", "https://hooks.example.com/a"},
			method: "POST", path: "/projects/*/integrations", success: "Created",
		},
	}
	statuses := []struct {
		status int
		body   string
		want   outcome
	}{
		{http.StatusForbidden, mockapi.Problem(http.StatusForbidden, "Access denied."),
			outcome{6, "Access denied."}},
		{http.StatusNotFound, mockapi.Problem(http.StatusNotFound, "Not here."), outcome{255, "404 Not Found"}},
		{http.StatusConflict, mockapi.Problem(http.StatusConflict, "Conflicting change."),
			outcome{255, "409 Conflict"}},
		{http.StatusInternalServerError, `{"status":"error","code":500,"message":"Internal Server Error"}`,
			outcome{255, "500 Internal Server Error"}},
		{http.StatusBadGateway, "<html><body><h1>502 Bad Gateway</h1></body></html>",
			outcome{255, "502 Bad Gateway"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f, api, projectID := c.setup(t)
			args := slices.Clone(c.args)
			if i := slices.Index(args, "PROJECT"); i >= 0 {
				args[i] = projectID
			}
			for _, s := range statuses {
				t.Run(http.StatusText(s.status), func(t *testing.T) {
					want, handled := c.handled[s.status]
					switch {
					case handled:
					case c.reports:
						want = outcome{1, fmt.Sprintf("The API returned %d %s for %s", s.status,
							http.StatusText(s.status), c.method)}
					default:
						want = s.want
					}
					before := len(api.Requests(c.method, c.path))
					api.FailNext(c.method, c.path, s.status, s.body)
					stdout, stderr, err := f.forSubtest(t).RunCombinedOutput(args...)
					assert.Greater(t, len(api.Requests(c.method, c.path)), before, "the write request was made")
					assert.Contains(t, stderr, want.want)
					assertCleanAPIError(t, stderr)
					if want.code == 0 {
						assert.NoError(t, err)
						return
					}
					assertExitCode(t, want.code, err)
					assert.NotContains(t, stderr, c.success)
					assert.Empty(t, stdout)
				})
			}
		})
	}
}

// TestAPIErrorHTMLBodies checks errors with an HTML body, e.g. from a proxy, in commands that read the API's errors.
func TestAPIErrorHTMLBodies(t *testing.T) {
	t.Parallel()
	api, domainsFactory, domainsProjectID := setUpDomains(t)
	p := setUpProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil))
	p.api.Get("/projects/"+layoutProjectID+"/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"integrations": map[string]any{
			"enabled": true,
			"config":  map[string]any{"webhook": map[string]any{"enabled": true}},
		}})
	})
	integrationsPath := "/projects/" + layoutProjectID + "/integrations/"
	p.api.SetProjectIntegrations(layoutProjectID, []*mockapi.Integration{
		{ID: "hook", Type: "webhook", URL: "https://hooks.example.com/a",
			Links: mockapi.MakeHALLinks("self="+integrationsPath+"hook", "#edit="+integrationsPath+"hook")},
		{ID: "gl", Type: "gitlab",
			Links: mockapi.MakeHALLinks("self="+integrationsPath+"gl", "#validate="+integrationsPath+"gl/validate")},
	})

	cases := []struct {
		name   string
		f      *cmdFactory
		h      *mockapi.Handler
		args   []string
		method string
		path   string
		status int
	}{
		{"domain:add 400", domainsFactory, api, []string{"domain:add", "-p", domainsProjectID, "-e", "main", "example.com"},
			"POST", "/projects/*/environments/main/domains", http.StatusBadRequest},
		{"domain:add 402", domainsFactory, api, []string{"domain:add", "-p", domainsProjectID, "-e", "main", "example.com"},
			"POST", "/projects/*/environments/main/domains", http.StatusPaymentRequired},
		{"domain:add 409", domainsFactory, api, []string{"domain:add", "-p", domainsProjectID, "-e", "main", "example.com"},
			"POST", "/projects/*/environments/main/domains", http.StatusConflict},
		{"integration:add 400", p.f, p.api, []string{"integration:add", "-p", layoutProjectID, "--type", "webhook",
			"--url", "https://hooks.example.com/b"}, "POST", "/projects/*/integrations", http.StatusBadRequest},
		{"integration:update 400", p.f, p.api, []string{"integration:update", "-p", layoutProjectID, "hook",
			"--url", "https://hooks.example.com/c"}, "PATCH", "/projects/*/integrations/hook", http.StatusBadRequest},
		{"integration:validate 400", p.f, p.api, []string{"integration:validate", "-p", layoutProjectID, "gl"},
			"POST", "/projects/*/integrations/gl/validate", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.h.FailNext(c.method, c.path, c.status, "<html><body>Error</body></html>")
			_, stderr, err := c.f.forSubtest(t).RunCombinedOutput(c.args...)
			assertExitCode(t, 255, err)
			assert.Contains(t, stderr, fmt.Sprintf("The API returned %d %s for %s", c.status, http.StatusText(c.status),
				c.method))
			assert.NotContains(t, stderr, "json_decode")
			assertCleanAPIError(t, stderr)
		})
	}
}
