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
  - applications.Foo_.type: type not found: 'invalid'; it must be one of: composable, dotnet, elixir, frankenphp, golang, java, nodejs, php, python, ruby, rust (check the Registry for supported types, or make an application using a composable image)
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
    stack: {runtimes: ["python@3.14"]}
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

func TestLint_ContainerFeatures(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		wantErrors string
	}{
		{
			name: "egress, container profiles, operations and cron timeouts",
			content: `
applications:
  app:
    type: php:8.4
    container_profile: HIGH_CPU
    relationships:
      db: "db:mysql"
    egress:
      build:
        allowed_domains:
          - host: repo.packagist.org
      runtime:
        allowed_domains:
          - host: api.example.com
            port: 443
    web:
      container_profile: BALANCED
      egress:
        runtime:
          allowed_domains:
            - host: api.example.com
    workers:
      queue:
        container_profile: HIGH_MEMORY
        commands:
          start: php worker.php
        egress:
          runtime:
            allowed_domains:
              - host: queue.example.com
    crons:
      report:
        spec: "0 * * * *"
        timeout: 600
        commands:
          start: php report.php
    operations:
      rebuild:
        role: admin
        timeout: 3600
        commands:
          start: php rebuild.php
          stop: pkill -f rebuild.php
services:
  db:
    type: mariadb:11.4
    container_profile: HIGH_MEMORY
tasks:
  agent:
    type: python:3.14
    container_profile: HIGH_CPU
    run:
      command: python agent.py
    egress:
      runtime:
        allowed_domains:
          - host: api.example.com
`,
		},
		{
			name: "structured stack forms",
			content: `
applications:
  listed:
    type: composable:25.11
    stack:
      runtimes: ["php@8.4", {"nodejs@24": {}}]
      packages: [yarn, {package: jq, channel: unstable}]
  scalar:
    type: composable:25.11
    stack:
      runtimes: php@8.4
      packages: yarn
routes:
  "https://{default}/":
    type: upstream
    upstream: "listed:http"
`,
		},
		{
			name: "empty stacks",
			content: `
applications:
  empty_list:
    type: composable:25.11
    stack: []
  empty_map:
    type: composable:25.11
    stack: {}
  empty_runtimes:
    type: composable:25.11
    stack: {runtimes: [], packages: []}
  classic:
    type: php:8.4
    stack: []
routes:
  "https://{default}/":
    type: upstream
    upstream: "empty_list:http"
`,
		},
		{
			name: "OCI images",
			content: `
applications:
  named:
    type: docker:1
    image:
      name: ghcr.io/example/app:1.0
  built:
    type: docker:1
    image:
      buildfile: Dockerfile
tasks:
  job:
    type: docker:1
    image:
      name: ghcr.io/example/job:1.0
    run:
      command: ./job
routes:
  "https://{default}/":
    type: upstream
    upstream: "named:http"
`,
		},
		{
			name: "invalid container features",
			content: `
applications:
  app:
    type: composable:25.11
    stack: {runtimes: ["php@8.4"]}
    egress:
      runtime:
        allowed_domains:
          - port: 443
    crons:
      report:
        spec: "0 * * * *"
        timeout: 100000
        commands:
          start: php report.php
`,
			wantErrors: `linter errors:
  - applications.app.crons.report.timeout: Must be less than or equal to 86400
  - applications.app.egress.runtime.allowed_domains.0: host is required`,
		},
		{
			name: "stack list",
			content: `
applications:
  app:
    type: composable:25.11
    stack: ["php@8.4"]
`,
			wantErrors: `linter errors:
  - applications.app.stack: 'stack' must be a mapping of 'runtimes' and 'packages', not a list`,
		},
		{
			name: "stack list with a semantic error",
			content: `
applications:
  Foo_:
    type: composable:25.11
    stack: ["php@8.4"]
`,
			wantErrors: `linter errors:
  - applications.Foo_.stack: 'stack' must be a mapping of 'runtimes' and 'packages', not a list
  - applications.Foo_: "Foo_" is not a valid application name, it can only contain lowercase alphanumeric characters, dashes, or underscores.`, //nolint:lll
		},
		{
			name: "invalid images",
			content: `
applications:
  none:
    type: docker:1
  both:
    type: docker:1
    image:
      name: example/app
      buildfile: Dockerfile
  classic:
    type: php:8.4
    image:
      name: example/app
routes:
  "https://{default}/":
    type: upstream
    upstream: "classic:http"
`,
			wantErrors: `linter errors:
  - applications.both.image: either name or buildfile must be provided, not both
  - applications.classic.image: 'image' is only used with a docker: type
  - applications.none.image: either name or buildfile must be provided, both are missing`,
		},
		{
			name: "container overrides",
			content: `
applications:
  app:
    type: php:8.4
    relationships:
      db: "db:mysql"
    web:
      access: {ssh: admin}
      additional_hosts: {api.example.com: 192.0.2.1}
      authorizations: [{type: env, action: view}]
      container_profile: BALANCED
      egress:
        runtime: {allowed_domains: [{host: api.example.com}]}
      mounts:
        web: {source: storage, source_path: web}
      operations:
        warm: {commands: {start: php warm.php}}
      relationships:
        database: "db:mysql"
      timezone: Europe/Paris
      variables: {env: {ROLE: web}}
    workers:
      queue:
        commands:
          start: php worker.php
        access: {ssh: admin}
        additional_hosts: {queue.example.com: 192.0.2.2}
        authorizations: [{type: env, action: view}]
        container_profile: HIGH_MEMORY
        egress:
          runtime: {allowed_domains: [{host: queue.example.com}]}
        mounts:
          queue: {source: tmp}
        operations:
          drain: {commands: {start: php drain.php}}
        relationships:
          database: "db:mysql"
        timezone: Europe/Paris
        variables: {env: {ROLE: worker}}
services:
  db:
    type: mariadb:11.4
`,
		},
		{
			name: "invalid container overrides",
			content: `
applications:
  app:
    type: php:8.4
    firewall:
      outbound: [{ips: ["192.0.2.0/24"]}]
    web:
      additional_hosts: {api.example.com: 1}
      operations:
        warm: {role: admin}
      firewall:
        outbound: [{ips: ["192.0.2.0/24"]}]
    workers:
      queue:
        commands:
          start: php worker.php
        egress:
          build: {allowed_domains: [{host: repo.packagist.org}]}
        firewall:
          outbound: [{ips: ["192.0.2.0/24"]}]
services:
  db:
    type: mariadb:11.4
    firewall:
      outbound: [{ips: ["192.0.2.0/24"]}]
`,
			wantErrors: `linter errors:
  - applications.app.web.additional_hosts.api.example.com: Invalid type. Expected: string, given: integer
  - applications.app.web.operations.warm: commands is required
  - applications.app.web: Additional property firewall is not allowed
  - applications.app.workers.queue.egress: Additional property build is not allowed
  - applications.app.workers.queue: Additional property firewall is not allowed
  - applications.app: Additional property firewall is not allowed
  - services.db: Additional property firewall is not allowed`,
		},
		{
			name: "web build egress",
			content: `
applications:
  app:
    type: php:8.4
    web:
      egress:
        build: {allowed_domains: [{host: repo.packagist.org}]}
`,
			wantErrors: `linter errors:
  - applications.app.web.egress: Additional property build is not allowed`,
		},
		{
			name: "invalid container profile override",
			content: `
applications:
  app:
    type: php:8.4
    web:
      container_profile: 5
`,
			wantErrors: `linter errors:
  - applications.app.web.container_profile: Invalid type. Expected: [string,null], given: integer`,
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

// Configs that the platform accepts with the Upsun settings.
func TestLint_PlatformLenience(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		wantErrors string
	}{
		{
			name: "null optional fields",
			content: `
applications:
  app:
    type: php:8.4
    timezone: null
    hooks: {build: null, deploy: null, post_deploy: null}
    runtime: {extensions: null, sizing_hints: {request_memory: null}, xdebug: null}
    source: {root: null}
    mounts:
      files: {source: storage, service: null}
    web:
      size: null
      mounts: null
      relationships: null
      operations: null
      commands: {start: null}
      upstream: {protocol: null}
      locations:
        /:
          root: null
          passthru: null
          index: null
          request_buffering: {max_request_size: null}
          rules:
            '\.css$': {expires: null, allow: null}
    workers:
      queue:
        size: null
        egress: null
        commands: {start: php worker.php, pre_start: null}
    crons:
      report:
        spec: "0 * * * *"
        shutdown_timeout: null
        commands: {start: php report.php, stop: null}
    operations:
      op:
        timeout: null
        commands: {start: echo, stop: null}
    egress:
      runtime:
        allowed_domains:
          - host: api.example.com
            port: null
routes:
  "https://{default}/":
    type: upstream
    upstream: app:http
    id: null
    primary: null
    tls:
      strict_transport_security: {enabled: null, preload: null}
tasks:
  agent:
    type: python:3.14
    timezone: null
    egress: null
    run: {command: ./run}
`,
		},
		{
			name: "null where the platform rejects it",
			content: `
applications:
  app:
    type: php:8.4
    mounts: null
    web:
      locations:
        /: {allow: null}
`,
			wantErrors: `linter errors:
  - applications.app.mounts: Invalid type. Expected: object, given: null
  - applications.app.web.locations./.allow: Invalid type. Expected: boolean, given: null`,
		},
		{
			name: "keys starting with a dot",
			content: `
.defaults: &defaults
  type: php:8.4
applications:
  .base: &base
    type: php:8.4
    web:
      locations:
        /: {root: public}
  app:
    <<: *base
    .note: x
    relationships: {db: "db:mysql"}
    web:
      .note: x
      commands: {.note: x, start: php -S 0:8080}
      locations:
        /:
          .note: x
          root: public
          request_buffering: {.note: x}
          rules:
            '\.css$': {.note: x, allow: true}
    mounts:
      files: {.note: x, source: storage}
    crons:
      report:
        .note: x
        spec: "0 * * * *"
        commands: {.note: x, start: php report.php}
    workers:
      queue:
        .note: {anchor: true}
        commands: {start: php worker.php}
    egress:
      .note: x
      runtime:
        .note: x
        allowed_domains: [{host: api.example.com, .note: x}]
services:
  .note: x
  db:
    .note: x
    type: mariadb:11.4
routes:
  .note: x
  "https://{default}/":
    .note: x
    type: upstream
    upstream: app:http
    cache: {.note: x, enabled: false}
`,
		},
		{
			name: "keys starting with a dot where the platform reads them",
			content: `
applications:
  app:
    type: php:8.4
    crons:
      .note: x
tasks:
  .note: x
`,
			wantErrors: `linter errors:
  - applications.app.crons..note: Invalid type. Expected: object, given: string
  - tasks..note: Invalid type. Expected: object, given: string`,
		},
		{
			name: "integer max_request_size and dynamic_compression",
			content: `
applications:
  app:
    type: php:8.4
    web:
      locations:
        /:
          root: public
          dynamic_compression: true
          request_buffering: {enabled: true, max_request_size: 5242880}
          rules:
            '\.css$': {dynamic_compression: false}
`,
		},
		{
			name: "no applications",
			content: `
routes:
  "https://{default}/":
    type: redirect
    to: https://example.com/
`,
		},
		{
			name: "custom container profile",
			content: `
applications:
  app:
    type: php:8.4
    container_profile: CUSTOM
    relationships: {db: "db:mysql"}
services:
  db:
    type: mariadb:11.4
    container_profile: CUSTOM
`,
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
