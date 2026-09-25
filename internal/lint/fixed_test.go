package lint

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:lll
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
			name: "rule overriding passthru with a boolean",
			files: map[string]string{
				".platform.app.yaml": `name: myapp
type: "php:8.4"
web:
  locations:
    "/":
      root: public
      passthru: /index.php
      rules:
        '\.php$':
          passthru: false`,
			},
			wantNoErr: true,
		},
		{
			name: "app with runtime operations",
			files: map[string]string{
				".platform.app.yaml": `name: myapp
type: "php:8.4"
operations:
  clear-cache:
    role: admin
    commands:
      start: php bin/console cache:clear`,
			},
			wantNoErr: true,
		},
		{
			name: "app with env authorization",
			files: map[string]string{
				".platform.app.yaml": `name: myapp
type: "php:8.4"
authorizations:
  - type: env
    action: view`,
			},
			wantNoErr: true,
		},
		{
			// Tasks are only available with Flex-style configuration.
			name: "app authorizing a task",
			files: map[string]string{
				".platform.app.yaml": `name: myapp
type: "php:8.4"
authorizations:
  - type: task
    resource: myagent
    action: operate`,
			},
			wantErrors: []string{"applications.myapp.authorizations.0.resource: task 'myagent' is not found (no tasks are defined)"},
		},
		{
			name: "composable image with type and stack",
			files: map[string]string{
				".platform.app.yaml": `name: myapp
type: "composable:25.11"
stack: ["php@8.3"]`,
			},
			wantNoErr: true,
		},
		{
			name: "composable image with stack and no type",
			files: map[string]string{
				".platform.app.yaml": `name: myapp
stack: ["php@8.3"]`,
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
			name: "applications.yaml map form must not set name in value",
			files: map[string]string{
				".platform/applications.yaml": `frontend:
  name: frontend
  type: "php:8.3"`,
			},
			wantErrors: []string{"the application name must not be set here"},
		},
		{
			name: "wrong style guard",
			files: map[string]string{
				".platform.app.yaml": `applications:
  foo:
    type: "php:8.3"`,
			},
			wantErrors: []string{"looks like Flex configuration in a Fixed-style file"},
		},
		{
			name: "app file missing required name",
			files: map[string]string{
				".platform.app.yaml": `type: "php:8.3"`,
			},
			wantErrors: []string{".platform.app.yaml:1: applications.app: name is required"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				writeFile(t, filepath.Join(dir, name), content)
			}

			result, style, err := CheckDir(dir, upsunVendor())
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
	// A multi-app layout: shared config at the root, one app per subdirectory.
	writeFile(t, filepath.Join(dir, ".platform", "routes.yaml"), "{}")
	writeFile(t, filepath.Join(dir, "a", ".platform.app.yaml"), "name: same\ntype: \"php:8.3\"")
	writeFile(t, filepath.Join(dir, "b", ".platform.app.yaml"), "name: same\ntype: \"php:8.3\"")
	result, _, err := CheckDir(dir, upsunVendor())
	require.NoError(t, err)
	// The error should name both source files so it is actionable.
	assert.Contains(t, result.String(), `duplicate application name "same"`)
	assert.Contains(t, result.String(), filepath.Join("a", ".platform.app.yaml"))
	assert.Contains(t, result.String(), filepath.Join("b", ".platform.app.yaml"))
}
