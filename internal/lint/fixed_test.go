package lint

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintDir_Fixed(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantErrors []string
		wantNoErr  bool
	}{
		{
			name: "valid single app",
			files: map[string]string{
				".platform.app.yaml": `name: myapp
type: "php:8.3"
relationships:
  database: "db:postgresql"`,
				".platform/services.yaml": `db:
  type: "postgresql:16"`,
				".platform/routes.yaml": `"https://{default}/":
  type: upstream
  upstream: "myapp:http"`,
			},
			wantNoErr: true,
		},
		{
			name: "invalid type and bad upstream",
			files: map[string]string{
				".platform.app.yaml": `name: myapp
type: "php:999"`,
				".platform/routes.yaml": `"https://{default}/":
  type: upstream
  upstream: "missing:http"`,
			},
			wantErrors: []string{
				"applications.myapp.type: version '999' is not supported",
				"upstream target 'missing' does not exist",
			},
		},
		{
			name: "applications.yaml map form",
			files: map[string]string{
				".platform/applications.yaml": `frontend:
  type: "php:8.3"`,
			},
			wantNoErr: true,
		},
		{
			name: "applications.yaml list form requires route for multiple apps",
			files: map[string]string{
				".platform/applications.yaml": `- name: frontend
  type: "php:8.3"
- name: backend
  type: "php:8.3"`,
			},
			wantErrors: []string{"at least 1 route must be defined when multiple applications are defined"},
		},
		{
			name: "wrong style guard",
			files: map[string]string{
				".platform.app.yaml": `applications:
  foo:
    type: "php:8.3"`,
			},
			wantErrors: []string{"looks like Flex (.upsun) configuration in a Fixed-style file"},
		},
		{
			name: "app file missing required name",
			files: map[string]string{
				".platform.app.yaml": `type: "php:8.3"`,
			},
			wantErrors: []string{".platform.app.yaml: name is required"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				writeFile(t, filepath.Join(dir, name), content)
			}

			result, style, err := CheckDir(context.Background(), dir)
			require.NoError(t, err)
			assert.Equal(t, StyleFixed, style)

			if tc.wantNoErr {
				assert.False(t, result.HasErrors(), "expected no errors, got: %s", result)
				return
			}
			assert.True(t, result.HasErrors())
			for _, want := range tc.wantErrors {
				assert.Contains(t, result.String(), want)
			}
		})
	}
}

func TestLintFixed_DuplicateAppName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a", ".platform.app.yaml"), "name: same\ntype: \"php:8.3\"")
	writeFile(t, filepath.Join(dir, "b", ".platform.app.yaml"), "name: same\ntype: \"php:8.3\"")
	result, _, err := CheckDir(context.Background(), dir)
	require.NoError(t, err)
	assert.Contains(t, result.String(), `duplicate application name "same"`)
}
