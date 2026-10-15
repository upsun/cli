package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// tokenState holds mutable token state shared between the test and HTTP handlers.
type tokenState struct {
	mu           sync.Mutex
	validToken   string
	tokenFetches int
}

func (s *tokenState) setToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validToken = token
}

func (s *tokenState) getToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validToken
}

// fetchToken increments the fetch count and returns the current valid token.
func (s *tokenState) fetchToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenFetches++
	return s.validToken
}

func (s *tokenState) getFetches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenFetches
}

func TestApiCurlCommand(t *testing.T) {
	t.Parallel()
	state := &tokenState{validToken: "valid-token"}

	mux := chi.NewMux()
	mux.Use(mockapi.RequestLogger(t))
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/oauth2") {
				if r.Header.Get("Authorization") != "Bearer "+state.getToken() {
					w.WriteHeader(http.StatusUnauthorized)
					//nolint:lll
					_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_token", "error_description": "Invalid access token."})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	mux.Post("/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		tok := state.fetchToken()
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "expires_in": 900, "token_type": "bearer"})
	})
	mux.Get("/users/me", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "userID", "email": "me@example.com"})
	})
	mux.Get("/fake-api-path", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("success"))
	})
	mockServer := httptest.NewServer(mux)
	defer mockServer.Close()

	f := newCommandFactory(t, mockServer.URL, mockServer.URL)

	// Load the first token.
	assert.Equal(t, "success", f.Run("api:curl", "/fake-api-path"))
	assert.Equal(t, 1, state.getFetches())

	// Revoke the access token and try the command again.
	// The old token should be considered invalid, so the API call should return 401,
	// and then the CLI should refresh the token and retry.
	state.setToken("new-valid-token")
	assert.Equal(t, "success", f.Run("api:curl", "/fake-api-path"))
	assert.Equal(t, 2, state.getFetches())

	assert.Equal(t, "success", f.Run("api:curl", "/fake-api-path"))
	assert.Equal(t, 2, state.getFetches())

	// If --no-retry-401 and --fail are provided then the command should return exit code 22.
	state.setToken("another-new-valid-token")
	stdOut, _, err := f.RunCombinedOutput("api:curl", "/fake-api-path", "--no-retry-401", "--fail")
	exitErr := &exec.ExitError{}
	assert.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 22, exitErr.ExitCode())
	assert.Empty(t, stdOut)
	assert.Equal(t, 2, state.getFetches())
}

type recordedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   string
}

// requestRecorder records the requests a handler receives.
type requestRecorder struct {
	mu   sync.Mutex
	reqs []recordedRequest
	next http.Handler
}

func (r *requestRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	r.mu.Lock()
	r.reqs = append(r.reqs, recordedRequest{
		Method: req.Method, Path: req.URL.Path, Header: req.Header.Clone(), Body: string(body),
	})
	r.mu.Unlock()
	r.next.ServeHTTP(w, req)
}

func (r *requestRecorder) requests() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.reqs)
}

// requestsTo returns the recorded requests to a path.
func (r *requestRecorder) requestsTo(path string) []recordedRequest {
	return slices.DeleteFunc(r.requests(), func(req recordedRequest) bool { return req.Path != path })
}

// TestApiCurl_RejectsHost checks that a path with a host is refused, so the access token is not sent to another host.
func TestApiCurl_RejectsHost(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiRecorder := &requestRecorder{next: mockapi.NewHandler(t)}
	apiServer := httptest.NewServer(apiRecorder)
	defer apiServer.Close()
	otherRecorder := &requestRecorder{next: http.NotFoundHandler()}
	otherServer := httptest.NewServer(otherRecorder)
	defer otherServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	otherHost := strings.TrimPrefix(otherServer.URL, "http://")
	for _, path := range []string{otherServer.URL + "/steal", "//" + otherHost + "/steal"} {
		stdout, stderr, err := f.RunCombinedOutput("api:curl", path)
		assertExitCode(t, 1, err)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "Invalid path: "+path)
	}
	assert.Empty(t, otherRecorder.requests())
	assert.Empty(t, apiRecorder.requests())
}

func TestApiCurl_Options(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.HandleFunc("/curl-test", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("success"))
	})
	apiRecorder := &requestRecorder{next: apiHandler}
	apiServer := httptest.NewServer(apiRecorder)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assert.Equal(t, "success", f.Run("api:curl", "/curl-test", "-X", "PATCH",
		"--json", `{"foo":"bar"}`, "-H", "X-Test: one", "-H", "X-Other: two"))
	reqs := apiRecorder.requestsTo("/curl-test")
	require.Len(t, reqs, 1)
	assert.Equal(t, "PATCH", reqs[0].Method)
	assert.JSONEq(t, `{"foo":"bar"}`, reqs[0].Body)
	assert.Equal(t, "application/json", reqs[0].Header.Get("Content-Type"))
	assert.Equal(t, "application/json", reqs[0].Header.Get("Accept"))
	assert.Equal(t, "one", reqs[0].Header.Get("X-Test"))
	assert.Equal(t, "two", reqs[0].Header.Get("X-Other"))
	assert.Equal(t, "Bearer access-token-1", reqs[0].Header.Get("Authorization"))

	_, stderr, err := f.RunCombinedOutput("api:curl", "/curl-test", "--json", "{invalid")
	assert.Error(t, err)
	assert.Contains(t, stderr, "The value of --json contains invalid JSON.")
	assert.Len(t, apiRecorder.requestsTo("/curl-test"), 1)
}

// TestApiCurl_CensorsToken checks that the access token is replaced by "[token]" in verbose output.
func TestApiCurl_CensorsToken(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.HandleFunc("/curl-test", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("success"))
	})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	for _, verbosity := range []string{"-v", "-vv"} {
		stdout, stderr, err := f.RunCombinedOutput("api:curl", "/curl-test", verbosity)
		require.NoError(t, err)
		assert.Equal(t, "success", stdout)
		assert.Contains(t, stderr, "Running command: curl -H 'Authorization: Bearer [token]'")
		assert.NotContains(t, stderr, "access-token-1")
		if verbosity == "-vv" {
			// The curl --verbose output.
			assert.Contains(t, stderr, "> Authorization: Bearer [token]")
		}
	}
}

// TestCurlCommands_BasePath checks the base path of each of the curl commands.
func TestCurlCommands_BasePath(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiRecorder := &requestRecorder{next: apiHandler}
	apiServer := httptest.NewServer(apiRecorder)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	envPath := "/projects/" + projectID + "/environments/main"
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})
	apiHandler.SetOrgs([]*mockapi.Org{makeOrg("org-id-1", "org-1", "Org 1", "my-user-id", "flexible")})

	cases := []struct {
		args []string
		path string
	}{
		{[]string{"api:curl"}, "/curl-test"},
		{[]string{"project:curl", "-p", projectID}, "/projects/" + projectID + "/curl-test"},
		{[]string{"environment:curl", "-p", projectID, "-e", "main"}, envPath + "/curl-test"},
		{[]string{"organization:curl", "-o", "org-1"}, "/organizations/org-id-1/curl-test"},
	}
	for _, c := range cases {
		apiHandler.HandleFunc(c.path, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("success"))
		})
	}

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	for _, c := range cases {
		assert.Equal(t, "success", f.Run(append(c.args, "/curl-test")...), c.args[0])
		assert.Len(t, apiRecorder.requestsTo(c.path), 1, c.args[0])
	}
}
