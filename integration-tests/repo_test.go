package tests

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

type gitTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// serveGitRepository serves a project with a "main" environment, and a
// repository through the Git Data API. Commit "c2" has the parent "c1".
func serveGitRepository(t *testing.T) (f *cmdFactory, projectID string) {
	apiHandler := mockapi.NewHandler(t)
	projectID = mockapi.ProjectID()
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

	commits := map[string]map[string]any{
		"c1": {"tree": "t-old", "parents": []string{}, "message": "Initial commit"},
		"c2": {"tree": "t-root", "parents": []string{"c1"}, "message": "Add src"},
	}
	trees := map[string][]gitTreeEntry{
		"t-old": {{"README.md", "100644", "blob", "b-readme-old"}},
		"t-root": {
			{"README.md", "100644", "blob", "b-readme"},
			{"src", "040000", "tree", "t-src"},
		},
		"t-src": {{"main.go", "100644", "blob", "b-main"}},
	}
	blobs := map[string]string{
		"b-readme-old": "Old readme\n",
		"b-readme":     "Readme\n",
		"b-main":       "package main\n",
	}
	gitPath := "/projects/" + projectID + "/git"
	apiHandler.Get(gitPath+"/commits/{sha}", func(w http.ResponseWriter, r *http.Request) {
		sha := chi.URLParam(r, "sha")
		c, ok := commits[sha]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        sha,
			"sha":       sha,
			"tree":      c["tree"],
			"parents":   c["parents"],
			"message":   c["message"],
			"author":    map[string]any{"date": "2014-04-01T10:00:00+00:00", "name": "Mock User", "email": "mock@example.com"},
			"committer": map[string]any{"date": "2014-04-01T10:00:00+00:00", "name": "Mock User", "email": "mock@example.com"},
			"_links":    mockapi.MakeHALLinks("self=/api" + gitPath + "/commits/" + sha),
		})
	})
	apiHandler.Get(gitPath+"/trees/{sha}", func(w http.ResponseWriter, r *http.Request) {
		sha := chi.URLParam(r, "sha")
		tree, ok := trees[sha]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     sha,
			"sha":    sha,
			"tree":   tree,
			"_links": mockapi.MakeHALLinks("self=/api" + gitPath + "/trees/" + sha),
		})
	})
	apiHandler.Get(gitPath+"/blobs/{sha}", func(w http.ResponseWriter, r *http.Request) {
		sha := chi.URLParam(r, "sha")
		content, ok := blobs[sha]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       sha,
			"sha":      sha,
			"size":     len(content),
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString([]byte(content)),
			"_links":   mockapi.MakeHALLinks("self=/api" + gitPath + "/blobs/" + sha),
		})
	})

	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", apiHandler))
	mux.Handle("/", apiHandler)
	apiServer := httptest.NewServer(mux)
	t.Cleanup(apiServer.Close)

	return newCommandFactory(t, apiServer.URL, authServer.URL), projectID
}

func TestCommitGet(t *testing.T) {
	t.Parallel()
	f, projectID := serveGitRepository(t)

	assertTrimmed(t, "Add src", f.Run("commit:get", "-p", projectID, "-e", "main", "c2", "-P", "message"))
	// Parent syntax.
	assertTrimmed(t, "c1", f.Run("commit:get", "-p", projectID, "-e", "main", "c2^", "-P", "sha"))
	assertTrimmed(t, "Mock User", f.Run("commit:get", "-p", projectID, "-e", "main", "c2", "-P", "author.name"))

	_, stderr, err := f.RunCombinedOutput("commit:get", "-p", projectID, "-e", "main", "c1^")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Commit not found: c1^")

	_, stderr, err = f.RunCombinedOutput("commit:get", "-p", projectID, "-e", "main", "missing")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Commit not found: missing")
}

func TestRepo(t *testing.T) {
	t.Parallel()
	f, projectID := serveGitRepository(t)
	run := func(args ...string) (stdout, stderr string, err error) {
		return f.RunCombinedOutput(append(args, "-p", projectID, "-e", "main")...)
	}

	cases := []struct {
		name       string
		args       []string
		wantStdout string
	}{
		{"cat", []string{"repo:cat", "README.md", "-c", "c2"}, "Readme\n"},
		{"cat nested", []string{"repo:cat", "src/main.go", "-c", "c2"}, "package main\n"},
		{"cat parent commit", []string{"repo:cat", "README.md", "-c", "c2^"}, "Old readme\n"},
		{"ls", []string{"repo:ls", "-c", "c2"}, "README.md\nsrc/\n"},
		{"ls subdirectory", []string{"repo:ls", "src", "-c", "c2"}, "main.go\n"},
		{"ls directories", []string{"repo:ls", "-d", "-c", "c2"}, "src/\n"},
		{"ls files", []string{"repo:ls", "-f", "-c", "c2"}, "README.md\n"},
		{
			"ls git style", []string{"repo:ls", "--git-style", "-c", "c2"},
			"100644 blob b-readme\tREADME.md\n040000 tree t-src\tsrc\n",
		},
		{"read file", []string{"repo:read", "src/main.go", "-c", "c2"}, "package main\n"},
		{"read directory", []string{"repo:read", "src", "-c", "c2"}, "main.go\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := run(c.args...)
			require.NoError(t, err, stderr)
			assert.Equal(t, c.wantStdout, stdout)
		})
	}

	t.Run("errors", func(t *testing.T) {
		_, stderr, err := run("repo:cat", "src", "-c", "c2")
		assertExitCode(t, 3, err)
		assert.Contains(t, stderr, "The requested file is a directory: src")
		assert.Contains(t, stderr, "To list directory contents, run: platform-test repo:ls [path]")

		_, stderr, err = run("repo:cat", "missing.txt", "-c", "c2")
		assertExitCode(t, 2, err)
		assert.Contains(t, stderr, "File not found: missing.txt")

		_, stderr, err = run("repo:ls", "missing", "-c", "c2")
		assertExitCode(t, 2, err)
		assert.Contains(t, stderr, "Directory not found: missing")

		_, stderr, err = run("repo:read", "missing", "-c", "c2")
		assertExitCode(t, 2, err)
		assert.Contains(t, stderr, "File or directory not found: missing")
	})
}
