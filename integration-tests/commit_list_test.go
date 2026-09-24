package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestCommitList checks that --limit, which Symfony passes as a string, is
// accepted by commit:list.
func TestCommitList(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	// The Git Data API client derives its base URL from an environment URL
	// containing "/api/projects/", so serve the mock API under /api too.
	env := makeEnv(projectID, "main", "production", "active", nil)
	env.Links["self"] = mockapi.HALLink{HREF: "/api/projects/" + projectID + "/environments/main"}
	apiHandler.SetEnvironments([]*mockapi.Environment{env})

	// A linear history: c3 -> c2 -> c1.
	parents := map[string][]string{"c3": {"c2"}, "c2": {"c1"}, "c1": {}}
	apiHandler.Get("/projects/"+projectID+"/git/commits/{sha}", func(w http.ResponseWriter, r *http.Request) {
		sha := chi.URLParam(r, "sha")
		p, ok := parents[sha]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      sha,
			"sha":     sha,
			"parents": p,
			"message": fmt.Sprintf("Commit %s", sha),
			"author": map[string]any{
				"date":  "2014-04-01T10:00:00+00:00",
				"name":  "Mock User",
				"email": "mock@example.com",
			},
			"_links": map[string]any{
				"self": map[string]any{"href": "/projects/" + projectID + "/git/commits/" + sha},
			},
		})
	})

	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", apiHandler))
	mux.Handle("/", apiHandler)
	apiServer := httptest.NewServer(mux)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	cases := []struct {
		name     string
		limit    string
		expected []string
	}{
		{"limit 1", "1", []string{"c3"}},
		{"limit 2", "2", []string{"c3", "c2"}},
		{"limit above history", "5", []string{"c3", "c2", "c1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdOut, stdErr, err := f.RunCombinedOutput("commit:list", "-p", projectID, "-e", "main", "c3",
				"--limit", c.limit, "--format", "csv", "--columns", "sha", "--no-header")
			require.NoError(t, err, "stderr: %s", stdErr)
			assertTrimmed(t, strings.Join(c.expected, "\n"), stdOut)
		})
	}

	t.Run("invalid limit", func(t *testing.T) {
		_, stdErr, err := f.RunCombinedOutput("commit:list", "-p", projectID, "-e", "main", "c3", "--limit", "abc")
		assert.Error(t, err)
		assert.Contains(t, stdErr, "The --limit value must be a non-negative integer.")
	})
}
