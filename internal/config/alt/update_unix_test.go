//go:build unix

package alt_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/config/alt"
)

// TestUpdateKeepsMode checks that an update keeps a shared config readable under a restrictive umask.
func TestUpdateKeepsMode(t *testing.T) {
	tempDir := t.TempDir()
	testConfigFilename := filepath.Join(tempDir, "config.yaml")
	require.NoError(t, os.WriteFile(testConfigFilename, testConfig, 0o600))
	require.NoError(t, os.Chmod(testConfigFilename, 0o644))
	hourAgo := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(testConfigFilename, hourAgo, hourAgo))

	cnf, err := config.FromYAML(testConfig)
	require.NoError(t, err)
	t.Setenv(cnf.Application.EnvPrefix+"HOME", tempDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(testConfigWithMetadata("{version: 1.0.1}"))
	}))
	defer server.Close()
	cnf.SourceFile = testConfigFilename
	cnf.Metadata.URL = server.URL + "/config.yaml"

	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)

	require.NoError(t, alt.Update(config.ToContext(context.Background(), cnf), cnf, func(string, ...any) {}))

	stat, err := os.Stat(testConfigFilename)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), stat.Mode().Perm())
}
