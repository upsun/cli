package lint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestDetectStyle(t *testing.T) {
	t.Run("flex", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), "applications: {}")
		assert.True(t, hasFlexConfig(dir))
		assert.False(t, hasFixedConfig(dir))
	})

	t.Run("fixed via app file", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".platform.app.yaml"), "name: app")
		assert.False(t, hasFlexConfig(dir))
		assert.True(t, hasFixedConfig(dir))
	})

	t.Run("fixed via .platform dir", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".platform", "routes.yaml"), "{}")
		assert.True(t, hasFixedConfig(dir))
	})

	t.Run("none", func(t *testing.T) {
		dir := t.TempDir()
		assert.False(t, hasFlexConfig(dir))
		assert.False(t, hasFixedConfig(dir))
	})
}

func TestLintDir_Flex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), `applications:
  app:
    type: "php:8.3"
    relationships:
      database: "db:postgresql"
services:
  db:
    type: "postgresql:16"
routes:
  "https://{default}/":
    type: upstream
    upstream: "app:http"
`)
	result, style, err := CheckDir(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, StyleFlex, style)
	assert.False(t, result.HasErrors(), "expected no errors, got: %s", result)
}

func TestLintDir_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, style, err := CheckDir(context.Background(), dir)
	require.Error(t, err)
	assert.Equal(t, StyleUnknown, style)
	assert.Contains(t, err.Error(), "no Upsun configuration found")
}

func TestLintDir_BothPresentPrefersFlex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), `applications:
  app:
    type: "php:8.3"
`)
	writeFile(t, filepath.Join(dir, ".platform.app.yaml"), "name: app")
	result, style, err := CheckDir(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, StyleFlex, style)
	assert.True(t, result.HasWarnings())
	assert.Contains(t, result.String(), "both .upsun and .platform")
}
