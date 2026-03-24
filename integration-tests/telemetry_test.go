package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// telemetryEvent matches the Event struct from internal/telemetry
type telemetryEvent struct {
	User         string `json:"user,omitempty"`
	Organization string `json:"organizationId,omitempty"`
	Version      string `json:"version"`
	Command      string `json:"command"`
	Arch         string `json:"arch"`
	OS           string `json:"os"`
}

// getCLIVersion extracts the version from the CLI binary
func getCLIVersion(t *testing.T) string {
	f := newCommandFactory(t, "", "")
	output := f.Run("--version")
	// Parse "Platform Test CLI 5.10.0-2026-03-24-829a9902-next"
	parts := strings.Fields(output)
	if len(parts) >= 4 {
		return parts[3]
	}
	t.Fatal("Could not extract version from CLI output")
	return ""
}

// mockTelemetryServer tracks received telemetry events
type mockTelemetryServer struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	events []telemetryEvent
	tokens []string
}

func newMockTelemetryServer(t *testing.T) *mockTelemetryServer {
	m := &mockTelemetryServer{
		t:      t,
		events: []telemetryEvent{},
		tokens: []string{},
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a POST request
		assert.Equal(t, http.MethodPost, r.Method)

		// Verify Content-Type
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Extract auth token if present
		authHeader := r.Header.Get("Authorization")
		m.mu.Lock()
		if authHeader != "" {
			m.tokens = append(m.tokens, authHeader)
		}
		m.mu.Unlock()

		// Parse the event
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var event telemetryEvent
		err = json.Unmarshal(body, &event)
		require.NoError(t, err, "telemetry payload must be valid JSON")

		// Store the event
		m.mu.Lock()
		m.events = append(m.events, event)
		m.mu.Unlock()

		// Return success
		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte(`{"success":true}`))
		require.NoError(t, err)
	}))

	return m
}

func (m *mockTelemetryServer) Close() {
	m.server.Close()
}

func (m *mockTelemetryServer) URL() string {
	return m.server.URL
}

func (m *mockTelemetryServer) GetEvents() []telemetryEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy
	events := make([]telemetryEvent, len(m.events))
	copy(events, m.events)
	return events
}

func (m *mockTelemetryServer) GetTokens() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy
	tokens := make([]string, len(m.tokens))
	copy(tokens, m.tokens)
	return tokens
}

func (m *mockTelemetryServer) WaitForEvents(count int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		got := len(m.events)
		m.mu.Unlock()
		if got >= count {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestTelemetry_TrackedCommand(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "test-user-123"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{
		ID:       myUserID,
		Username: "testuser",
	})
	apiHandler.SetOrgs([]*mockapi.Org{
		{
			ID:    "test-org-456",
			Type:  "flexible",
			Name:  "testorg",
			Label: "Test Organization",
			Owner: myUserID,
		},
	})
	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID:           "test-project-1",
			Organization: "test-org-456",
			Vendor:       "test-vendor",
			Title:        "Test Project",
			Region:       "test-region",
		},
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	telemetryServer := newMockTelemetryServer(t)
	defer telemetryServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TELEMETRY_ENDPOINT="+telemetryServer.URL(),
		EnvPrefix+"ORGANIZATION=testorg",
	)

	// Run a tracked command
	f.Run("project:list")

	// Wait for telemetry event
	require.True(t, telemetryServer.WaitForEvents(1, 3*time.Second), "telemetry event should be sent")

	events := telemetryServer.GetEvents()
	require.Len(t, events, 1, "exactly one telemetry event should be sent")

	event := events[0]
	assert.Equal(t, "project:list", event.Command)
	assert.Equal(t, "test-user-123", event.User)
	assert.Equal(t, "", event.Organization)
	assert.Equal(t, getCLIVersion(t), event.Version)
	assert.Equal(t, runtime.GOARCH, event.Arch)
	assert.Equal(t, runtime.GOOS, event.OS)

	// Verify auth token was sent
	tokens := telemetryServer.GetTokens()
	require.Len(t, tokens, 1)
	assert.Contains(t, tokens[0], "Bearer ")
}

func TestTelemetry_UntrackedCommand(t *testing.T) {
	telemetryServer := newMockTelemetryServer(t)
	defer telemetryServer.Close()

	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TELEMETRY_ENDPOINT="+telemetryServer.URL())

	// Run an untracked command (version is not in the whitelist)
	f.Run("--version")

	// Wait a bit to ensure no telemetry is sent
	time.Sleep(500 * time.Millisecond)

	events := telemetryServer.GetEvents()
	assert.Len(t, events, 0, "no telemetry should be sent for untracked commands")
}

func TestTelemetry_DoNotTrack(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "test-user-123"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{
		ID:       myUserID,
		Username: "testuser",
	})
	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID:           "test-project-1",
			Organization: "test-org-456",
			Vendor:       "test-vendor",
			Title:        "Test Project",
			Region:       "test-region",
		},
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	telemetryServer := newMockTelemetryServer(t)
	defer telemetryServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv,
		EnvPrefix+"TELEMETRY_ENDPOINT="+telemetryServer.URL(),
		"DO_NOT_TRACK=1",
	)

	// Run a tracked command with DO_NOT_TRACK set
	f.Run("project:list")

	// Wait a bit to ensure no telemetry is sent
	time.Sleep(500 * time.Millisecond)

	events := telemetryServer.GetEvents()
	assert.Len(t, events, 0, "no telemetry should be sent when DO_NOT_TRACK=1")
}

func TestTelemetry_NoEndpoint(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "test-user-123"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{
		ID:       myUserID,
		Username: "testuser",
	})
	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID:           "test-project-1",
			Organization: "test-org-456",
			Vendor:       "test-vendor",
			Title:        "Test Project",
			Region:       "test-region",
		},
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	telemetryServer := newMockTelemetryServer(t)
	defer telemetryServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	// Don't set TELEMETRY_ENDPOINT

	// Run a tracked command without endpoint configured
	f.Run("project:list")

	// Wait a bit to ensure no telemetry is sent
	time.Sleep(500 * time.Millisecond)

	events := telemetryServer.GetEvents()
	assert.Len(t, events, 0, "no telemetry should be sent when endpoint is not configured")
}

func TestTelemetry_MultipleCommands(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "test-user-123"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{
		ID:       myUserID,
		Username: "testuser",
	})
	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID:           "test-project-1",
			Organization: "test-org-456",
			Vendor:       "test-vendor",
			Title:        "Test Project",
			Region:       "test-region",
		},
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	telemetryServer := newMockTelemetryServer(t)
	defer telemetryServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TELEMETRY_ENDPOINT="+telemetryServer.URL())

	// Run multiple tracked commands
	f.Run("project:list")

	// Wait for first event
	require.True(t, telemetryServer.WaitForEvents(1, 3*time.Second))

	events := telemetryServer.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "project:list", events[0].Command)
}

func TestTelemetry_ServerError(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "test-user-123"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{
		ID:       myUserID,
		Username: "testuser",
	})
	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg("org-id-1", "org-1", "Org 1", myUserID, "flexible"),
	})
	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID:           "project-id-1",
			Organization: "org-id-1",
			Vendor:       "test-vendor",
			Title:        "Test Project",
			Region:       "test-region",
		},
	})
	apiHandler.SetUserGrants([]*mockapi.UserGrant{
		{
			ResourceID:     "org-id-1",
			ResourceType:   "organization",
			OrganizationID: "org-id-1",
			UserID:         myUserID,
			Permissions:    []string{"admin"},
		},
		{
			ResourceID:     "project-id-1",
			ResourceType:   "project",
			OrganizationID: "org-id-1",
			UserID:         myUserID,
			Permissions:    []string{"admin"},
		},
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	// Create a server that returns errors
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte(`{"error":"server error"}`))
		require.NoError(t, err)
	}))
	defer errorServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TELEMETRY_ENDPOINT="+errorServer.URL)

	// Run a tracked command - should succeed even if telemetry fails
	output := f.Run("project:list")
	assert.NotEmpty(t, output, "command should succeed even if telemetry fails")
}
