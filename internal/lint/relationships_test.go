package lint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/lint"
)

//nolint:lll
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
				"\n  - services.foo: no application or task has a relationship to service 'foo'",
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
		{
			name: "service_used_by_mounts",
			// A network-storage service used only through mounts is not unused.
			content: `
applications:
  app:
    type: php:8.4
    mounts:
      /files:
        source: service
        service: files
tasks:
  agent:
    type: python:3.14
    mounts:
      /shared:
        source: service
        service: shared
services:
  files:
    type: network-storage:2.0
  shared:
    type: network-storage:2.0`,
		},
		{
			name: "task_relationships",
			// A service used only by a task is not reported as unused.
			content: `
applications:
  app:
    type: php:8.4
services:
  db:
    type: mariadb:11.4
tasks:
  agent:
    type: python:3.14
    relationships:
      database: "db:mysql"
      site: "app:http"
      implicit:
        service: db`,
		},
		{
			name: "task_relationship_not_found",
			content: `
applications:
  app:
    type: php:8.4
tasks:
  agent:
    type: python:3.14
    relationships:
      cache:`,
			expectErrorMessage: `linter errors:
  - tasks.agent.relationships.cache: relationship 'cache' in task 'agent' does not match any service (or app) (did you forget to define services?)`, //nolint:lll
		},
		{
			name: "relationship_to_task",
			content: `
applications:
  app:
    type: php:8.4
    relationships:
      agent: "agent:http"
tasks:
  agent:
    type: python:3.14
  other:
    type: python:3.14
    relationships:
      agent:`,
			expectErrorMessage: `linter errors:
  - applications.app.relationships.agent: relationship 'agent' in application 'app' points to task 'agent', but a task cannot be a relationship target
  - tasks.other.relationships.agent: relationship 'agent' in task 'other' points to task 'agent', but a task cannot be a relationship target`, //nolint:lll
		},
		{
			name: "task_name_collisions",
			content: `
applications:
  app:
    type: php:8.4
    relationships:
      db:
services:
  db:
    type: mariadb:11.4
tasks:
  app:
    type: python:3.14
  db:
    type: python:3.14`,
			expectErrorMessage: `linter errors:
  - tasks.app: duplicate name found: 'app' in 'tasks' (previous in 'applications')
  - tasks.db: duplicate name found: 'db' in 'tasks' (previous in 'services')`,
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
