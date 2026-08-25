package alt_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/config/alt"
	"github.com/upsun/cli/internal/state"
)

func TestUpdate(t *testing.T) {
	tempDir := t.TempDir()

	// Copy test config to a temporary directory, and fake its modification time.
	testConfigFilename := filepath.Join(tempDir, "config.yaml")
	err := os.WriteFile(testConfigFilename, testConfig, 0o600)
	require.NoError(t, err)

	cnf, err := config.FromYAML(testConfig)
	require.NoError(t, err)

	// Set up state so that it stays in a temporary directory.
	t.Setenv(cnf.Application.EnvPrefix+"HOME", tempDir)

	// Set up the config to be updated via a test HTTP server.
	remoteConfig := testConfig
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/config.yaml" {
			_, _ = w.Write(remoteConfig)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cnf.SourceFile = testConfigFilename
	cnf.Updates.CheckInterval = 1
	cnf.Metadata.URL = server.URL + "/config.yaml"

	// TODO use test context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = config.ToContext(ctx, cnf)

	var lastLogged string
	logger := func(msg string, args ...any) {
		lastLogged = fmt.Sprintf(msg, args...)
	}

	assert.True(t, alt.ShouldUpdate(cnf))

	err = alt.Update(ctx, cnf, logger)
	assert.NoError(t, err)
	assert.Contains(t, lastLogged, "Config file updated recently")

	hourAgo := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(testConfigFilename, hourAgo, hourAgo))

	err = alt.Update(ctx, cnf, logger)
	assert.NoError(t, err)
	assert.Contains(t, lastLogged, "Automatically updated config file")

	err = alt.Update(ctx, cnf, logger)
	assert.NoError(t, err)
	assert.Contains(t, lastLogged, "Config updates checked recently")

	// Reset the LastChecked time and file modified time.
	resetTimes := func() {
		s, err := state.Load(cnf)
		require.NoError(t, err)
		s.ConfigUpdates.LastChecked = 0
		require.NoError(t, state.Save(s, cnf))
		require.NoError(t, os.Chtimes(testConfigFilename, hourAgo, hourAgo))
	}
	resetTimes()

	remoteConfig = append(remoteConfig, []byte("\nmetadata: {version: 1.0.1}")...)
	// A local version that cannot be parsed says nothing about whether the new
	// config is newer, so the update proceeds rather than failing.
	cnf.Metadata.Version = "invalid"
	err = alt.Update(ctx, cnf, logger)
	assert.NoError(t, err)
	assert.Contains(t, lastLogged, "Automatically updated config file")
	resetTimes()
	cnf.Metadata.Version = "1.0.1"
	err = alt.Update(ctx, cnf, logger)
	assert.NoError(t, err)
	assert.Contains(t, lastLogged, "Config is already up to date (version 1.0.1)")

	resetTimes()

	updated := time.Now()
	cnf.Metadata.Version = ""
	cnf.Metadata.UpdatedAt = updated
	remoteConfig = testConfig
	remoteConfig = append(remoteConfig,
		[]byte(fmt.Sprintf("\nmetadata: {updated_at: %s}", updated.Add(-time.Minute).Format(time.RFC3339)))...)
	err = alt.Update(ctx, cnf, logger)
	assert.NoError(t, err)
	assert.Contains(t, lastLogged, "Config is already up to date")
}

func TestShouldUpdate(t *testing.T) {
	testConfigFilename := "/tmp/mock/path/to/config.yaml"

	cnf, err := config.FromYAML(testConfig)
	require.NoError(t, err)

	cnf.Updates.Check = true
	cnf.SourceFile = testConfigFilename
	cnf.Metadata.URL = "https://example.com/config.yaml"
	assert.True(t, alt.ShouldUpdate(cnf))

	cnf.Updates.Check = false
	assert.False(t, alt.ShouldUpdate(cnf))

	cnf.Updates.Check = true
	cnf.SourceFile = ""
	assert.False(t, alt.ShouldUpdate(cnf))

	cnf.SourceFile = testConfigFilename
	cnf.Metadata.URL = ""
	assert.False(t, alt.ShouldUpdate(cnf))
}

// testConfigWithMetadata copies the test config and appends a metadata section.
// It copies because testConfig is shared by every test in the package.
func testConfigWithMetadata(metadata string) []byte {
	cnf := make([]byte, 0, len(testConfig)+len("\nmetadata: ")+len(metadata))
	cnf = append(cnf, testConfig...)
	cnf = append(cnf, "\nmetadata: "...)
	return append(cnf, metadata...)
}

// TestUpdateWithUnusableLocalVersion checks that a config with a URL but no
// usable version is still updated, which is what clears the bad version.
func TestUpdateWithUnusableLocalVersion(t *testing.T) {
	for _, localVersion := range []string{"", "invalid", "1.2.3.4"} {
		t.Run("local version "+strconv.Quote(localVersion), func(t *testing.T) {
			tempDir := t.TempDir()
			testConfigFilename := filepath.Join(tempDir, "config.yaml")
			require.NoError(t, os.WriteFile(testConfigFilename, testConfig, 0o600))
			hourAgo := time.Now().Add(-time.Hour)
			require.NoError(t, os.Chtimes(testConfigFilename, hourAgo, hourAgo))

			cnf, err := config.FromYAML(testConfig)
			require.NoError(t, err)
			t.Setenv(cnf.Application.EnvPrefix+"HOME", tempDir)

			remoteConfig := testConfigWithMetadata("{version: 1.0.1}")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(remoteConfig)
			}))
			defer server.Close()

			cnf.SourceFile = testConfigFilename
			cnf.Metadata.URL = server.URL + "/config.yaml"
			cnf.Metadata.Version = localVersion

			var lastLogged string
			err = alt.Update(config.ToContext(context.Background(), cnf), cnf,
				func(msg string, args ...any) { lastLogged = fmt.Sprintf(msg, args...) })
			assert.NoError(t, err)
			assert.Contains(t, lastLogged, "Automatically updated config file")

			// The new version is now on disk, so the next run compares cleanly.
			b, err := os.ReadFile(testConfigFilename)
			require.NoError(t, err)
			updated, err := config.FromYAML(b)
			require.NoError(t, err)
			assert.Equal(t, "1.0.1", updated.Metadata.Version)
		})
	}
}

// TestUpdateWithUnusableNewVersion checks that a served config whose version
// will not parse is rejected while fetching and never applied.
func TestUpdateWithUnusableNewVersion(t *testing.T) {
	tempDir := t.TempDir()
	testConfigFilename := filepath.Join(tempDir, "config.yaml")
	localConfig := testConfigWithMetadata("{version: 1.0.0}")
	require.NoError(t, os.WriteFile(testConfigFilename, localConfig, 0o600))
	hourAgo := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(testConfigFilename, hourAgo, hourAgo))

	cnf, err := config.FromYAML(localConfig)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", cnf.Metadata.Version)
	t.Setenv(cnf.Application.EnvPrefix+"HOME", tempDir)

	remoteConfig := testConfigWithMetadata("{version: not-a-version}")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(remoteConfig)
	}))
	defer server.Close()

	cnf.SourceFile = testConfigFilename
	cnf.Metadata.URL = server.URL + "/config.yaml"

	err = alt.Update(config.ToContext(context.Background(), cnf), cnf, func(string, ...any) {})
	assert.ErrorContains(t, err, "invalid config")

	b, err := os.ReadFile(testConfigFilename)
	require.NoError(t, err)
	assert.Equal(t, string(localConfig), string(b), "the local config must not be modified")
}
