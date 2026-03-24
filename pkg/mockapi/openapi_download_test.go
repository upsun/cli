package mockapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureOpenAPISpec(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping download test in short mode")
	}

	// Test with existing file
	t.Run("existing file", func(t *testing.T) {
		path, err := ensureOpenAPISpec()
		require.NoError(t, err)
		assert.NotEmpty(t, path)

		// Verify file exists and is valid JSON
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(data), "openapi")
	})

	// Test downloading to temp directory
	t.Run("download to temp", func(t *testing.T) {
		tempDir := t.TempDir()
		destPath := filepath.Join(tempDir, "openapi.json")

		err := downloadOpenAPISpec(DefaultOpenAPIURL, destPath)
		require.NoError(t, err)

		// Verify downloaded file exists
		info, err := os.Stat(destPath)
		require.NoError(t, err)
		assert.Greater(t, info.Size(), int64(1000), "Downloaded file should be > 1KB")

		// Verify it's valid JSON with OpenAPI content
		data, err := os.ReadFile(destPath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "openapi")
		assert.Contains(t, string(data), "paths")
	})

	// Test with custom URL via environment variable
	t.Run("custom URL via env var", func(t *testing.T) {
		// Save original env var
		originalURL := os.Getenv(OpenAPIURLEnvVar)
		defer func() {
			if originalURL != "" {
				os.Setenv(OpenAPIURLEnvVar, originalURL)
			} else {
				os.Unsetenv(OpenAPIURLEnvVar)
			}
		}()

		// Set custom URL (use the same URL for testing)
		os.Setenv(OpenAPIURLEnvVar, DefaultOpenAPIURL)

		tempDir := t.TempDir()
		destPath := filepath.Join(tempDir, "custom-openapi.json")

		// Get URL from env var and download
		url := os.Getenv(OpenAPIURLEnvVar)
		err := downloadOpenAPISpec(url, destPath)
		require.NoError(t, err)

		// Verify file was downloaded
		_, err = os.Stat(destPath)
		require.NoError(t, err)
	})
}

func TestDownloadOpenAPISpec_InvalidURL(t *testing.T) {
	tempDir := t.TempDir()
	destPath := filepath.Join(tempDir, "openapi.json")

	err := downloadOpenAPISpec("https://invalid-url-that-does-not-exist.example.com/openapi.json", destPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download")
}

func TestDownloadOpenAPISpec_404(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	tempDir := t.TempDir()
	destPath := filepath.Join(tempDir, "openapi.json")

	err := downloadOpenAPISpec("https://developer.upsun.com/does-not-exist.json", destPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}
