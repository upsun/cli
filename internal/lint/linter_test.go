package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:lll
func TestLint(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "combine errors",
			content: `
applications:
  Foo_:
    type: invalid:1.0
    relationships:
      missing_service:
    web:
      commands:
        start: "echo started"
services: {}
`,
			wantErr: true,
			wantErrMsg: `linter errors:
  - applications.Foo_.relationships.missing_service: relationship 'missing_service' in application 'Foo_' does not match any service (or app) (did you forget to define services?)
  - applications.Foo_.type: type not found: 'invalid'; it must be one of: composable, dotnet, elixir, golang, java, nodejs, php, python, ruby, rust (check the Registry for supported types, or make an application using a composable image)
  - applications.Foo_: "Foo_" is not a valid application name, it can only contain lowercase alphanumeric characters, dashes, or underscores.`, //nolint:lll
		},
		{
			name: "all ok",
			content: `
applications:
  foo:
    type: golang:1.26
    relationships:
      database:
    web:
      commands:
        start: "go run main.go"
services:
  database:
    type: mariadb:11.4
`,
		},
		{
			name: "service missing type",
			content: `
applications:
  foo:
    type: golang:1.26
    relationships:
      database:
    web:
      commands:
        start: "go run main.go"
services:
  database: {}
`,
			wantErr:    true,
			wantErrMsg: "linter errors:\n  - services.database: type is required",
		},
		{
			name: "invalid worker name",
			content: `
applications:
  foo:
    type: golang:1.26
    relationships:
      database:
    web:
      commands:
        start: "go run main.go"
    workers:
      _badworker:
        commands:
          start: echo ok
services:
  database:
    type: mariadb:11.4
`,
			wantErr: true,
			wantErrMsg: `linter errors:
  - applications.foo.workers._badworker: "_badworker" is not a valid worker name, it should start and end with alphanumeric characters.`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CheckContent(tc.content)
			assert.NoError(t, err)

			if tc.wantErr {
				assert.True(t, result.HasErrors(), "expected errors but got none")
				assert.Equal(t, tc.wantErrMsg, result.Error())
			} else {
				assert.False(t, result.HasErrors(), "expected no errors but got: %s", result.Error())
			}
		})
	}
}

func TestLint_Schema(t *testing.T) {
	cases := []struct {
		name string
		// content is merged Flex configuration.
		content string
		// wantErrors is the formatted error output, empty when none is expected.
		wantErrors string
	}{
		{
			// Based on the documented examples for tasks and authorizations.
			name: "tasks and authorizations",
			content: `
applications:
  myapp:
    type: python:3.14
    web:
      commands:
        start: python app.py
    relationships:
      database: "db:postgresql"
    mounts:
      config:
        source: local
        source_path: config
    authorizations:
      - type: task
        resource: myagent
        action: operate
      - type: env
        action: view
    workers:
      queue:
        commands:
          start: python queue.py
        authorizations:
          - type: env
            action: view
services:
  db:
    type: postgresql:18
tasks:
  myagent:
    type: python:3.14
    source:
      root: /agent
    hooks:
      build: pip install -r requirements.txt
    run:
      command: python setup.py && python agent.py
      timeout: 1200
    relationships:
      database: "db:postgresql"
      app: "myapp:http"
    mounts:
      cache:
        source: tmp
      shared:
        source: storage
        service: myapp
    variables:
      env:
        BATCH_SIZE: "100"
    authorizations:
      - type: env
        action: view
  composed:
    type: composable:25.11
    stack: ["python@3.14"]
    run:
      command: python run.py
  perf:
    base: performance-agent
routes:
  "https://{default}/":
    type: upstream
    upstream: "myapp:http"
`,
		},
		{
			name: "invalid authorization and task properties",
			content: `
applications:
  myapp:
    type: python:3.14
    web:
      commands:
        start: python app.py
    authorizations:
      - type: project
        action: view
        scope: all
tasks:
  myagent:
    type: python:3.14
    run:
      command: python agent.py
      timeout: 100000
    web:
      commands:
        start: python agent.py
`,
			wantErrors: `linter errors:
  - applications.myapp.authorizations.0.type: must be one of the following: "env", "task"
  - applications.myapp.authorizations.0: Additional property scope is not allowed
  - tasks.myagent.run.timeout: Must be less than or equal to 86400
  - tasks.myagent: Additional property web is not allowed`,
		},
		{
			name: "unknown top-level key",
			content: `
applications:
  myapp:
    type: php:8.4
workers: {}
`,
			wantErrors: `linter errors:
  - Additional property workers is not allowed`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := CheckContent(c.content)
			require.NoError(t, err)
			if c.wantErrors == "" {
				assert.False(t, result.HasErrors(), "unexpected errors: %s", result)
				assert.False(t, result.HasWarnings(), "unexpected warnings: %s", result)
				return
			}
			assert.Equal(t, c.wantErrors, result.Error())
		})
	}
}
