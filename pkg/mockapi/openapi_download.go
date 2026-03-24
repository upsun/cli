package mockapi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	// DefaultOpenAPIURL is the default URL for the Upsun OpenAPI specification
	DefaultOpenAPIURL = "https://developer.upsun.com/openapi.json"

	// OpenAPIURLEnvVar is the environment variable to override the OpenAPI URL
	OpenAPIURLEnvVar = "UPSUN_OPENAPI_URL"

	// OpenAPISpecFilename is the filename for the downloaded OpenAPI spec
	OpenAPISpecFilename = "upsun-openapi.json"
)

// downloadOpenAPISpec downloads the OpenAPI specification from the given URL
// and saves it to the specified path.
func downloadOpenAPISpec(url, destPath string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Download the spec
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download OpenAPI spec from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download OpenAPI spec: HTTP %d", resp.StatusCode)
	}

	// Create the destination file
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", destPath, err)
	}
	defer out.Close()

	// Write the response body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write OpenAPI spec to %s: %w", destPath, err)
	}

	return nil
}

// ensureOpenAPISpec ensures the OpenAPI spec file exists, downloading it if necessary.
// It returns the path to the spec file.
func ensureOpenAPISpec() (string, error) {
	// Try multiple possible paths for the spec file
	possiblePaths := []string{
		"pkg/mockapi/testdata/upsun-openapi.json",       // from repo root
		"testdata/upsun-openapi.json",                   // from pkg/mockapi
		"../pkg/mockapi/testdata/upsun-openapi.json",    // from integration-tests
		"../../pkg/mockapi/testdata/upsun-openapi.json", // from nested dirs
	}

	// Check if file already exists
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// File doesn't exist, download it
	url := os.Getenv(OpenAPIURLEnvVar)
	if url == "" {
		url = DefaultOpenAPIURL
	}

	// Determine the correct download path based on current working directory
	// If we can find testdata/ relative to current dir, use that; otherwise use full path
	destPath := "testdata/upsun-openapi.json"
	if info, err := os.Stat("testdata"); err != nil || !info.IsDir() {
		// testdata doesn't exist in current dir, try creating from repo root
		destPath = "pkg/mockapi/testdata/upsun-openapi.json"
	}

	if err := downloadOpenAPISpec(url, destPath); err != nil {
		return "", err
	}

	return destPath, nil
}

// refreshOpenAPISpec forces a re-download of the OpenAPI specification.
// This is useful when the spec has been updated upstream.
func refreshOpenAPISpec() error {
	url := os.Getenv(OpenAPIURLEnvVar)
	if url == "" {
		url = DefaultOpenAPIURL
	}

	destPath := "pkg/mockapi/testdata/upsun-openapi.json"
	return downloadOpenAPISpec(url, destPath)
}
