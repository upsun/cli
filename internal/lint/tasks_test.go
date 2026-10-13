package lint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/lint"
)

//nolint:lll
func TestCheckTasks(t *testing.T) {
	cases := []struct {
		name    string
		content string
		// wantErrors is the formatted error output, empty when none is expected.
		wantErrors string
	}{
		{
			name: "valid task with authorizations",
			content: `
applications:
  myapp:
    type: python:3.14
    authorizations:
      - type: task
        resource: myagent
        action: operate
      - type: env
        action: view
tasks:
  myagent:
    type: python:3.14
    run:
      command: python agent.py
    authorizations:
      - type: env
        action: view`,
		},
		{
			name: "built-in task definition",
			content: `
applications:
  myapp:
    type: python:3.14
tasks:
  perf:
    base: performance-agent`,
		},
		{
			name: "base with other properties",
			content: `
applications:
  myapp:
    type: python:3.14
tasks:
  perf:
    base: performance-agent
    run:
      command: ./run`,
			wantErrors: `linter errors:
  - tasks.perf.base: when 'base' is set, no other property is allowed`,
		},
		{
			name: "missing run command",
			content: `
applications:
  myapp:
    type: python:3.14
tasks:
  myagent:
    type: python:3.14`,
			wantErrors: `linter errors:
  - tasks.myagent.run.command: a run command is required`,
		},
		{
			name: "authorization actions must match their type",
			content: `
applications:
  myapp:
    type: python:3.14
    authorizations:
      - type: env
        action: operate
tasks:
  myagent:
    type: python:3.14
    run:
      command: ./run
    authorizations:
      - type: task
        resource: myagent
        action: view`,
			wantErrors: `linter errors:
  - applications.myapp.authorizations.0.action: authorization type 'env' only allows the action 'view'
  - tasks.myagent.authorizations.0.action: authorization type 'task' only allows the action 'operate'`,
		},
		{
			name: "task authorization without a resource",
			content: `
applications:
  myapp:
    type: python:3.14
    authorizations:
      - type: task
        action: operate`,
			wantErrors: `linter errors:
  - applications.myapp.authorizations.0.resource: a task authorization requires a resource (the task name)`,
		},
		{
			name: "task authorization for a missing task, on each container",
			content: `
applications:
  myapp:
    type: python:3.14
    authorizations:
      - type: task
        resource: missing
        action: operate
    web:
      authorizations:
        - type: task
          resource: missing
          action: operate
    workers:
      queue:
        commands:
          start: ./queue
        authorizations:
          - type: env
            action: view
          - type: task
            resource: missing
            action: operate
tasks:
  a:
    type: python:3.14
    run:
      command: ./a
  b:
    type: python:3.14
    run:
      command: ./b`,
			wantErrors: `linter errors:
  - applications.myapp.authorizations.0.resource: task 'missing' is not found (defined tasks: a, b)
  - applications.myapp.web.authorizations.0.resource: task 'missing' is not found (defined tasks: a, b)
  - applications.myapp.workers.queue.authorizations.1.resource: task 'missing' is not found (defined tasks: a, b)`,
		},
		{
			name: "task authorization with no tasks defined",
			content: `
applications:
  myapp:
    type: python:3.14
    authorizations:
      - type: task
        resource: myagent
        action: operate`,
			wantErrors: `linter errors:
  - applications.myapp.authorizations.0.resource: task 'myagent' is not found (no tasks are defined)`,
		},
		{
			name: "task mounts",
			content: `
applications:
  myapp:
    type: python:3.14
services:
  files:
    type: network-storage:2.0
tasks:
  myagent:
    type: python:3.14
    run:
      command: ./run
    mounts:
      cache:
        source: instance
      shared:
        source: storage
        service: myapp
      network:
        source: service
        service: files
      own:
        source: storage
      wrong_app:
        source: storage
        service: other
      wrong_service:
        source: service
        service: other`,
			wantErrors: `linter errors:
  - tasks.myagent.mounts.own: a task has no storage of its own, so a 'storage' mount must name an application in 'service'
  - tasks.myagent.mounts.wrong_app.service: application 'other' is not found
  - tasks.myagent.mounts.wrong_service.service: service 'other' is not found`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := lint.DecodeConfig(c.content)
			require.NoError(t, err)
			result := lint.CheckTasks(cfg)
			if c.wantErrors == "" {
				assert.False(t, result.HasErrors(), "unexpected errors: %s", result)
				return
			}
			assert.Equal(t, c.wantErrors, result.Error())
		})
	}
}
