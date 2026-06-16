package lint

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
    type: golang:1.25
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
    type: golang:1.25
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
    type: golang:1.25
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
			result, err := CheckContent(context.Background(), tc.content)
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
