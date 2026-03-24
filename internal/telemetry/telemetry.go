package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/legacy"
)

const defaultTimeout = 2 * time.Second

// Event represents a generic telemetry event payload.
type Event struct {
	User         string `json:"user,omitempty"`
	Organization string `json:"organizationId,omitempty"`
	Version      string `json:"version"`
	Command      string `json:"command"`
	Arch         string `json:"arch"`
	OS           string `json:"os"`
}

// SendTelemetryEvent sends a telemetry event asynchronously.
// It respects the DO_NOT_TRACK environment variable and fails silently on errors.
// Returns a channel that will be closed when the telemetry operation completes.
func SendTelemetryEvent(ctx context.Context, cnf *config.Config, command string) chan struct{} {
	done := make(chan struct{})

	// Respect DO_NOT_TRACK
	if os.Getenv("DO_NOT_TRACK") == "1" {
		close(done)
		return done
	}

	// Check if telemetry is enabled in config
	if !cnf.Telemetry.Enabled {
		close(done)
		return done
	}

	// Check if command is whitelisted
	if !IsTracked(command) {
		close(done)
		return done
	}

	// Get endpoint from config/environment
	endpoint := getEndpoint(cnf)
	if endpoint == "" {
		// Silently skip if no endpoint is configured
		close(done)
		return done
	}

	// Run in a goroutine to avoid blocking
	go func() {
		defer close(done)

		ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()

		// Create legacy wrapper to fetch user/org IDs and auth token
		wrapper := makeLegacyCLIWrapper(cnf)
		userID := getUserID(ctx, wrapper)
		orgID := getOrgID(ctx, wrapper)
		authToken := getAuthToken(ctx, wrapper)

		// Build the event
		event := &Event{
			User:         userID,
			Organization: orgID,
			Version:      config.Version,
			Command:      command,
			Arch:         runtime.GOARCH,
			OS:           runtime.GOOS,
		}

		// Send the event
		if err := sendEvent(ctx, endpoint, event, authToken); err != nil {
			// Fail silently - telemetry should never interfere with user experience
			return
		}
	}()

	return done
}

// sendEvent sends the event to the configured telemetry endpoint.
func sendEvent(ctx context.Context, endpoint string, event *Event, authToken string) error {
	// Marshal the event
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Add authorization header using CLI's auth token
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code (but don't fail on non-2xx since we fail silently anyway)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// getEndpoint returns the telemetry endpoint from config or environment.
func getEndpoint(cnf *config.Config) string {
	// Try environment variable first
	if endpoint := os.Getenv(cnf.Application.EnvPrefix + "TELEMETRY_ENDPOINT"); endpoint != "" {
		return endpoint
	}

	// Fall back to config
	return cnf.Telemetry.Endpoint
}

// makeLegacyCLIWrapper creates a legacy CLI wrapper for executing commands.
func makeLegacyCLIWrapper(cnf *config.Config) *legacy.CLIWrapper {
	return &legacy.CLIWrapper{
		Config:             cnf,
		Version:            config.Version,
		DisableInteraction: true,
		// No stdout/stderr/stdin - we'll override these per call
	}
}

// getUserID retrieves the user ID from the legacy CLI.
// Returns empty string if not authenticated or on error.
func getUserID(ctx context.Context, wrapper *legacy.CLIWrapper) string {
	var buf bytes.Buffer
	wrapper.Stdout = &buf
	wrapper.Stderr = nil
	wrapper.Stdin = nil

	if err := wrapper.Exec(ctx, "auth:info", "-P", "id", "--no-interaction"); err != nil {
		return "" // Return empty if not authenticated
	}

	return strings.TrimSpace(buf.String())
}

// getOrgID retrieves the organization ID from the legacy CLI.
// Returns empty string if no org context or on error.
func getOrgID(ctx context.Context, wrapper *legacy.CLIWrapper) string {
	var buf bytes.Buffer
	wrapper.Stdout = &buf
	wrapper.Stderr = nil
	wrapper.Stdin = nil

	if err := wrapper.Exec(ctx, "organization:info", "id", "--no-interaction"); err != nil {
		return "" // Return empty if no org context
	}

	return strings.TrimSpace(buf.String())
}

// getAuthToken retrieves the authentication token from the legacy CLI.
// Returns empty string if not authenticated or on error.
func getAuthToken(ctx context.Context, wrapper *legacy.CLIWrapper) string {
	var buf bytes.Buffer
	wrapper.Stdout = &buf
	wrapper.Stderr = nil
	wrapper.Stdin = nil

	if err := wrapper.Exec(ctx, "auth:token", "-W"); err != nil {
		return "" // Return empty if not authenticated
	}

	return strings.TrimSpace(buf.String())
}
