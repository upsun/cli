package lint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/lint"
)

func TestCheckRelationships(t *testing.T) {
	cases := []struct {
		name               string
		content            string
		expectErrorMessage string
	}{
		{
			name: "correct_single_short",
			// N.B. YAML requires spaces for indents, not tabs.
			content: `
applications:
  foo:
    relationships:
      database:
services:
  database:
    type: mariadb:11.4`,
		},
		{
			name: "incorrect_single_short",
			// N.B. YAML requires spaces for indents, not tabs.
			content: `
applications:
  foo:
    relationships:
      database:`,
			expectErrorMessage: "linter errors:\n  - applications.foo.relationships.database: relationship 'database' in application 'foo' " + //nolint:lll
				"does not match any service (or app) (did you forget to define services?)",
		},
		{
			name: "incorrect_single_explicit",
			content: `
applications:
  foo:
    relationships:
      database:
        service: mydb`,
			expectErrorMessage: "linter errors:\n  - applications.foo.relationships.database: relationship 'database' in application 'foo' " + //nolint:lll
				"points to a service (or app) named 'mydb' which is not found (did you forget to define services?)",
		},
		{
			name: "incorrect_duplicate_service_names",
			// N.B. YAML requires spaces for indents, not tabs.
			content: `
applications:
  foo: {}
services:
  foo: {}`,
			expectErrorMessage: "linter errors:" +
				"\n  - services.foo: duplicate name found: 'foo' in 'services' (previous in 'applications')" +
				"\n  - services.foo: no application has a relationship to service 'foo'",
		},
		{
			name: "correct_multiapp",
			content: `
applications:
  foo:
    relationships:
      database:
      cache:
        service: kv
  bar:
    relationships:
      database:
      foo:
services:
  database:
    type: mariadb:11.4
  kv:
    type: valkey:8.0`,
		},
		{
			name: "incorrect_multiapp",
			content: `
applications:
  foo:
    relationships:
      database:
      cache:
        service: kv
  bar:
    relationships:
      postgres:
services:
  database:
    type: mariadb:11.4
  kv:
    type: valkey:8.0`,
			expectErrorMessage: "linter errors:\n  - applications.bar.relationships.postgres: relationship 'postgres' in application 'bar' " + //nolint:lll
				"does not match any service (or app)",
		},
		{
			// Worker names are scoped to their application, so two applications
			// may each define a worker with the same name.
			name: "correct_duplicate_worker_names_across_apps",
			content: `
applications:
  foo:
    relationships:
      database:
    workers:
      queue:
        commands:
          start: "node worker.js"
  bar:
    relationships:
      database:
    workers:
      queue:
        commands:
          start: "node worker.js"
services:
  database:
    type: mariadb:11.4`,
		},
		{
			// A worker may share a name with a service without clashing.
			name: "correct_worker_name_matching_service",
			content: `
applications:
  foo:
    relationships:
      database:
    workers:
      database:
        commands:
          start: "node worker.js"
services:
  database:
    type: mariadb:11.4`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := lint.DecodeConfig(c.content)
			if err != nil {
				assert.Equal(t, c.expectErrorMessage, err.Error())
				return
			}
			result := lint.CheckRelationships(cfg)
			if c.expectErrorMessage != "" {
				assert.True(t, result.HasErrors() || result.HasWarnings())
				assert.Equal(t, c.expectErrorMessage, result.Error())
			} else {
				assert.False(t, result.HasErrors())
			}
		})
	}
}
