package tests

import (
	"net/http"
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
