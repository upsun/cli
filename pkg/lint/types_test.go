package lint_test

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/lint"
	"github.com/upsun/cli/pkg/registry"
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
			expectErrorMessage: "Linter errors:\n  - applications.foo.type: version '5.3' is not supported for type 'php'; " + //nolint:lll
				"it must be exactly one of: 8.4, 8.3, 8.2, 8.1",
		},
		{
			name: "not_found_type",
			content: `
applications:
  foo:
    type: strapi:latest`,
			expectErrorMessage: "Linter errors:\n  - applications.foo.type: type not found: 'strapi'; it must be one of: " +
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
			expectErrorMessage: "Linter errors:\n  - services.myservice.type: type 'nodejs' is a runtime type, not a service type", //nolint:lll
		},
		{
			name: "composable_without_stack",
			content: `
applications:
  foo:
    type: composable:26.05`,
			expectErrorMessage: `Linter warnings:
  - applications.foo: 'stack' should be specified when using a composable image`,
		},
		{
			name: "task_types",
			// A task using a built-in definition ('base') has no type to check.
			content: `
applications:
  foo:
    type: php:8.4
tasks:
  good:
    type: php:8.4
  perf:
    base: performance-agent
  bad:
    type: php:1.0`,
			expectErrorMessage: `Linter errors:
  - tasks.bad.type: version '1.0' is not supported for type 'php'; it must be exactly one of: 8.4, 8.3, 8.2, 8.1`,
		},
		{
			name: "task_without_type",
			content: `
applications:
  foo:
    type: php:8.4
tasks:
  bad:
    stack: {runtimes: ["php@8.4"]}`,
			expectErrorMessage: `Linter errors:
  - tasks.bad.type: type cannot be empty
Linter warnings:
  - tasks.bad.stack: 'stack' is only used with a composable image type`,
		},
		{
			name: "stack_with_non_composable_type",
			content: `
applications:
  foo:
    type: php:8.4
    stack: {runtimes: ["php@8.4"]}`,
			expectErrorMessage: `Linter warnings:
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
				assert.Equal(t, c.expectErrorMessage, result.String())
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
	assert.Equal(t, `Linter warnings:
  - applications.foo.type: version '1.25' of type 'golang' is retired; use one of: 1.27, 1.26`, result.String())
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

func TestCheckTypes_NewerVersion(t *testing.T) {
	reg := registry.Registry{
		"composable": {Type: "composable", IsRuntime: true, Versions: registry.VersionInfo{
			Supported: []string{"26.05"},
			Retired:   []string{"25.11"},
		}},
		"php": {Type: "php", IsRuntime: true, Versions: registry.VersionInfo{
			Supported: []string{"8.5", "8.4"},
			Legacy:    []string{"5.4"},
		}},
		"elasticsearch": {Type: "elasticsearch", Versions: registry.VersionInfo{Retired: []string{"7.2"}}},
	}

	cases := []struct {
		name    string
		t       string
		service bool
		warning string
		err     string
	}{
		{name: "newer_channel", t: "composable:26.11",
			warning: "version '26.11' of type 'composable' is newer than the known versions, so it could not be checked; " +
				"the newest known version is 26.05"},
		{name: "newer_minor", t: "php:8.10",
			warning: "version '8.10' of type 'php' is newer than the known versions, so it could not be checked; " +
				"the newest known version is 8.5"},
		{name: "newer_major", t: "php:9.0",
			warning: "version '9.0' of type 'php' is newer than the known versions, so it could not be checked; " +
				"the newest known version is 8.5"},
		{name: "typo", t: "php:84",
			err: "version '84' is not supported for type 'php'; it must be exactly one of: 8.5, 8.4"},
		{name: "two_majors_ahead", t: "php:10.0",
			err: "version '10.0' is not supported for type 'php'; it must be exactly one of: 8.5, 8.4"},
		{name: "more_parts", t: "php:8.6.1",
			err: "version '8.6.1' is not supported for type 'php'; it must be exactly one of: 8.5, 8.4"},
		{name: "signed", t: "php:+9.0",
			err: "version '+9.0' is not supported for type 'php'; it must be exactly one of: 8.5, 8.4"},
		{name: "overflow", t: "php:99999999999999999999.0",
			err: "version '99999999999999999999.0' is not supported for type 'php'; it must be exactly one of: 8.5, 8.4"},
		{name: "older_unknown", t: "composable:25.08",
			err: "version '25.08' is not supported for type 'composable'; it must be exactly one of: 26.05"},
		{name: "imprecise", t: "php:8",
			err: "version '8' is not precise enough for type 'php'; it must be exactly one of: 8.5, 8.4"},
		{name: "not_numeric", t: "php:latest",
			err: "version 'latest' is not supported for type 'php'; it must be exactly one of: 8.5, 8.4"},
		{name: "empty", t: "php",
			err: "version '' is not supported for type 'php'; it must be exactly one of: 8.5, 8.4"},
		{name: "no_supported_versions", t: "elasticsearch:9.0", service: true,
			err: "version '9.0' is not supported for type 'elasticsearch'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yaml, path := "applications:\n  app:\n    type: "+c.t+"\n    stack: []", "applications.app.type"
			if c.service {
				yaml, path = "services:\n  db:\n    type: "+c.t, "services.db.type"
			}
			cfg, err := lint.DecodeConfig(yaml)
			require.NoError(t, err)
			result := lint.CheckTypes(cfg, reg)
			if c.warning != "" {
				assert.Empty(t, result.Errors)
				assert.Equal(t, []lint.Issue{{Path: path, Message: c.warning}}, result.Warnings)
			} else {
				assert.Equal(t, []lint.Issue{{Path: path, Message: c.err}}, result.Errors)
			}
		})
	}
}
