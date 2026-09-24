package lint_test

import (
	_ "embed"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/lint"
	"github.com/upsun/cli/internal/lint/registry"
)

//go:embed testdata/registry.json
var registryJSON []byte

func TestCheckTypes(t *testing.T) {
	testRegistry, err := registry.Parse(registryJSON)
	require.NoError(t, err)

	cases := []struct {
		name               string
		content            string
		expectErrorMessage string
	}{
		{
			name: "correct",
			// N.B. YAML requires spaces for indents, not tabs.
			content: `
applications:
  foo:
    type: php:8.4
services:
  database:
    type: mariadb:11.4`,
		},
		{
			name: "legacy_redis",
			content: `
applications:
  foo:
    type: php:8.4
services:
  cache:
    type: redis:6.0`,
		},
		{
			name: "unsupported_php",
			content: `
applications:
  foo:
    type: php:5.3
services:
  database:
    type: mariadb:11.4`,
			expectErrorMessage: "linter errors:\n  - applications.foo.type: version '5.3' is not supported for type 'php'; " + //nolint:lll
				"it must be exactly one of: 8.4, 8.3, 8.2, 8.1",
		},
		{
			name: "not_found_type",
			content: `
applications:
  foo:
    type: strapi:latest`,
			expectErrorMessage: "linter errors:\n  - applications.foo.type: type not found: 'strapi'; it must be one of: " +
				"composable, dotnet, elixir, golang, java, nodejs, php, python, ruby, rust " +
				"(check the Registry for supported types, or make an application using a composable image)",
		},
		{
			name: "service_runtime_type",
			content: `
applications:
  foo:
    type: php:8.4
services:
  myservice:
    type: nodejs:22`,
			expectErrorMessage: "linter errors:\n  - services.myservice.type: type 'nodejs' is a runtime type, not a service type", //nolint:lll
		},
		{
			name: "composable_without_type",
			content: `
applications:
  foo:
    stack:
      - bun@1
      - ffmpeg`,
			expectErrorMessage: `linter warnings:
  - applications.foo: 'type' should be specified (as a composable image) when using 'stack'`,
		},
		{
			name: "composable_without_stack",
			content: fmt.Sprintf(`
applications:
  foo:
    type: composable:%s`, registry.ChannelStable),
			expectErrorMessage: `linter warnings:
  - applications.foo: 'stack' should be specified when using a composable image`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := lint.DecodeConfig(c.content)
			if err != nil {
				assert.FailNow(t, "decodeConfig failed", err)
			}
			result := lint.CheckTypes(cfg, testRegistry)
			if c.expectErrorMessage != "" {
				assert.True(t, result.HasErrors() || result.HasWarnings())
				assert.Equal(t, c.expectErrorMessage, result.Error())
			} else {
				assert.False(t, result.HasErrors())
			}
		})
	}
}
