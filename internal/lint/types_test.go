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
		{
			name: "stack_with_non_composable_type",
			content: `
applications:
  foo:
    type: php:8.4
    stack: ["php@8.4"]`,
			expectErrorMessage: `linter warnings:
  - applications.foo.stack: 'stack' is only used with a composable image type`,
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

func TestCheckTypes_RetiredVersion(t *testing.T) {
	reg := registry.Registry{
		"golang": {Type: "golang", IsRuntime: true, Versions: registry.VersionInfo{
			Supported: []string{"1.27", "1.26"},
			Retired:   []string{"1.25"},
		}},
	}
	cfg, err := lint.DecodeConfig(`
applications:
  foo:
    type: golang:1.25`)
	require.NoError(t, err)

	result := lint.CheckTypes(cfg, reg)
	assert.False(t, result.HasErrors())
	assert.Equal(t, `linter warnings:
  - applications.foo.type: version '1.25' of type 'golang' is retired; use one of: 1.27, 1.26`, result.Error())
}

func TestCheckTypes_NoSupportedVersions(t *testing.T) {
	reg := registry.Registry{
		"elasticsearch": {Type: "elasticsearch", Versions: registry.VersionInfo{Retired: []string{"7.2"}}},
	}
	cfg, err := lint.DecodeConfig(`
applications:
  app:
    type: golang:1.26
services:
  retired:
    type: elasticsearch:7.2
  unknown:
    type: elasticsearch:9.0`)
	require.NoError(t, err)

	result := lint.CheckTypes(cfg, reg)
	assert.Contains(t, result.Warnings, lint.Issue{
		Path: "services.retired.type", Message: "version '7.2' of type 'elasticsearch' is retired",
	})
	assert.Contains(t, result.Errors, lint.Issue{
		Path: "services.unknown.type", Message: "version '9.0' is not supported for type 'elasticsearch'",
	})
}
