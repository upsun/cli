package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/config"
)

func TestSendTelemetryEvent_RespectsDoNotTrack(_ *testing.T) {
	originalValue := os.Getenv("DO_NOT_TRACK")
	os.Setenv("DO_NOT_TRACK", "1")
	defer func() {
		if originalValue == "" {
			os.Unsetenv("DO_NOT_TRACK")
		} else {
			os.Setenv("DO_NOT_TRACK", originalValue)
		}
	}()

	cnf := &config.Config{}
	cnf.Telemetry.Enabled = true
	cnf.Telemetry.Endpoint = "http://localhost:8080"

	done := SendTelemetryEvent(context.Background(), cnf, "init")
	<-done // Should complete immediately without sending
}

func TestSendTelemetryEvent_RespectsDisabledConfig(_ *testing.T) {
	originalValue := os.Getenv("DO_NOT_TRACK")
	os.Unsetenv("DO_NOT_TRACK")
	defer func() {
		if originalValue != "" {
			os.Setenv("DO_NOT_TRACK", originalValue)
		}
	}()

	cnf := &config.Config{}
	cnf.Telemetry.Enabled = false
	cnf.Telemetry.Endpoint = "http://localhost:8080"

	done := SendTelemetryEvent(context.Background(), cnf, "init")
	<-done // Should complete immediately without sending
}

func TestSendTelemetryEvent_RequiresEndpoint(_ *testing.T) {
	cnf := &config.Config{}
	cnf.Telemetry.Enabled = true
	cnf.Telemetry.Endpoint = ""

	done := SendTelemetryEvent(context.Background(), cnf, "init")
	<-done // Should skip telemetry
}

func TestSendTelemetryEvent_OnlyTrackedCommands(t *testing.T) {
	cnf := &config.Config{}
	cnf.Telemetry.Enabled = true
	cnf.Telemetry.Endpoint = "http://localhost:8080"

	// Tracked command
	assert.True(t, IsTracked("init"))

	// Non-tracked command
	assert.False(t, IsTracked("version"))

	// Non-tracked command should complete immediately
	done := SendTelemetryEvent(context.Background(), cnf, "version")
	<-done
}

func TestIsTracked(t *testing.T) {
	cases := []struct {
		command string
		tracked bool
	}{
		{"init", true},
		{"project:init", true},
		{"project:create", true},
		{"environment:branch", true},
		{"project:delete", true},
		{"environment:delete", true},
		{"mount:upload", true},
		{"mount:download", true},
		{"version", false},
		{"list", false},
		{"help", false},
		{"unknown", false},
	}

	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			assert.Equal(t, tc.tracked, IsTracked(tc.command))
		})
	}
}

func TestExtractCommand(t *testing.T) {
	cases := []struct {
		args     []string
		expected string
	}{
		{[]string{"init"}, "init"},
		{[]string{"project:create", "--flag"}, "project:create"},
		{[]string{"environment:branch", "new-branch"}, "environment:branch"},
		{[]string{}, "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, ExtractCommand(tc.args))
		})
	}
}

func TestBuildEvent(t *testing.T) {
	event := &Event{
		User:         "user-123",
		Organization: "org-456",
		Version:      "1.0.0",
		Command:      "init",
		Arch:         "arm64",
		OS:           "darwin",
	}

	payload, err := json.Marshal(event)
	assert.NoError(t, err)
	assert.NotEmpty(t, payload)

	// Verify JSON structure
	var decoded map[string]any
	err = json.Unmarshal(payload, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "user-123", decoded["user"])
	assert.Equal(t, "org-456", decoded["organizationId"])
	assert.Equal(t, "1.0.0", decoded["version"])
	assert.Equal(t, "init", decoded["command"])
	assert.Equal(t, "arm64", decoded["arch"])
	assert.Equal(t, "darwin", decoded["os"])
}

func TestGetEndpoint(t *testing.T) {
	cnf := &config.Config{}
	cnf.Application.EnvPrefix = "TEST_CLI_"
	cnf.Telemetry.Endpoint = "http://config-endpoint.com"

	// Test config value
	assert.Equal(t, "http://config-endpoint.com", getEndpoint(cnf))

	// Test environment variable override
	os.Setenv("TEST_CLI_TELEMETRY_ENDPOINT", "http://env-endpoint.com")
	defer os.Unsetenv("TEST_CLI_TELEMETRY_ENDPOINT")
	assert.Equal(t, "http://env-endpoint.com", getEndpoint(cnf))
}
