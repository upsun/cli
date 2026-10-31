package init

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/upsun/whatsun/pkg/digest"

	"github.com/upsun/cli/internal/config"
)

// TestRunAIConfig_NoPrompt checks that RunAIConfig does not prompt, so that it can be retried after a login.
func TestRunAIConfig_NoPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".upsun"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".upsun", "config.yaml"), []byte("applications: {}\n"), 0o600))

	var stderr bytes.Buffer
	opts := &Options{APIURL: server.URL, IsInteractive: true}
	err := RunAIConfig(context.Background(), &config.Config{}, &digest.Digest{}, dir, opts, &bytes.Buffer{}, &stderr)
	assert.EqualError(t, err, "rate limit exceeded: please try again later")
	assert.NotContains(t, stderr.String(), "already exists")
}
