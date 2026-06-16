package lint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/lint"
)

func TestCheckRoutes(t *testing.T) {
	cases := []struct {
		name               string
		content            string
		expectErrorMessage string
	}{
		{
			name: "valid_upstream_to_app",
			content: `
applications:
  myapp:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream
    upstream: myapp:http`,
		},
		{
			name: "valid_upstream_to_service",
			content: `
services:
  api:
    type: mariadb:11.4
routes:
  "https://{default}/api":
    type: upstream
    upstream: api:http`,
		},
		{
			name: "valid_multiple_routes",
			content: `
applications:
  frontend:
    type: nodejs:22
  backend:
    type: php:8.4
services:
  database:
    type: postgresql:15
routes:
  "https://{default}/":
    type: upstream
    upstream: frontend:http
  "https://{default}/api":
    type: upstream
    upstream: backend:http
  "https://{default}/db":
    type: upstream
    upstream: database:http`,
		},
		{
			name: "valid_redirect_route",
			content: `
applications:
  myapp:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream
    upstream: myapp:http
  "https://www.{default}/":
    type: redirect
    to: "https://{default}/"`,
		},
		{
			name: "missing_upstream_property",
			content: `
applications:
  myapp:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream`,
			expectErrorMessage: "linter errors:\n  - routes[\"https://{default}/\"].upstream: upstream property is required for routes with type 'upstream'", //nolint:lll
		},
		{
			name: "invalid_upstream_format",
			content: `
applications:
  myapp:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream
    upstream: myapp_without_protocol`,
			expectErrorMessage: "linter errors:\n  - routes[\"https://{default}/\"].upstream: upstream 'myapp_without_protocol' must be in format 'name:protocol'", //nolint:lll
		},
		{
			name: "nonexistent_upstream_target",
			content: `
applications:
  myapp:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream
    upstream: nonexistent:http`,
			expectErrorMessage: "linter errors:\n  - routes[\"https://{default}/\"].upstream: upstream target 'nonexistent' does not exist, available targets: myapp", //nolint:lll
		},
		{
			name: "no_apps_or_services",
			content: `
routes:
  "https://{default}/":
    type: upstream
    upstream: myapp:http`,
			expectErrorMessage: "linter errors:\n  - routes[\"https://{default}/\"].upstream: upstream target 'myapp' does not exist (no applications or services defined)", //nolint:lll
		},
		{
			name: "multiple_invalid_routes",
			content: `
applications:
  myapp:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream
    upstream: nonexistent1:http
  "https://{default}/api":
    type: upstream
    upstream: nonexistent2:http`,
			expectErrorMessage: "linter errors:\n  - routes[\"https://{default}/\"].upstream: upstream target 'nonexistent1' does not exist, available targets: myapp\n  - routes[\"https://{default}/api\"].upstream: upstream target 'nonexistent2' does not exist, available targets: myapp", //nolint:lll
		},
		{
			name: "invalid_protocol_for_app",
			content: `
applications:
  myapp:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream
    upstream: myapp:fastcgi`,
			expectErrorMessage: "linter errors:\n  - routes[\"https://{default}/\"].upstream: protocol 'fastcgi' is not valid for application 'myapp'; must be 'http'", //nolint:lll
		},
		{
			name: "any_protocol_valid_for_service",
			content: `
services:
  database:
    type: postgresql:15
routes:
  "https://{default}/":
    type: upstream
    upstream: database:custom_protocol`,
		},

		{
			name: "custom_protocol_for_app_error",
			content: `
applications:
  myapp:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream
    upstream: myapp:custom_protocol`,
			expectErrorMessage: "linter errors:\n  - routes[\"https://{default}/\"].upstream: protocol 'custom_protocol' is not valid for application 'myapp'; must be 'http'", //nolint:lll
		},
		{
			name: "complex_route_urls",
			content: `
applications:
  app1:
    type: nodejs:22
  app2:
    type: php:8.4
routes:
  "https://site1.{default}/":
    type: upstream
    upstream: app1:http
  "https://site2.{default}/admin":
    type: upstream
    upstream: app2:http`,
		},
		{
			name: "single_app_no_routes_allowed",
			content: `
applications:
  myapp:
    type: php:8.4`,
		},
		{
			name: "multiple_apps_no_routes_error",
			content: `
applications:
  app1:
    type: nodejs:22
  app2:
    type: php:8.4`,
			expectErrorMessage: "linter errors:\n  - routes: at least 1 route must be defined when multiple applications are defined", //nolint:lll
		},
		{
			name: "multiple_apps_with_routes_valid",
			content: `
applications:
  app1:
    type: nodejs:22
  app2:
    type: php:8.4
routes:
  "https://{default}/":
    type: upstream
    upstream: app1:http`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := lint.DecodeConfig(c.content)
			if err != nil {
				assert.FailNow(t, "DecodeConfig failed", err)
			}
			result := lint.CheckRoutes(cfg)
			if c.expectErrorMessage != "" {
				assert.True(t, result.HasErrors() || result.HasWarnings())
				assert.Equal(t, c.expectErrorMessage, result.Error())
			} else {
				assert.False(t, result.HasErrors())
				assert.False(t, result.HasWarnings())
			}
		})
	}
}
