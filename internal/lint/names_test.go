package lint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/lint"
)

func TestCheckNames(t *testing.T) {
	cases := []struct {
		name              string
		content           string
		expectErrorValues []string
	}{
		{
			name: "valid_names",
			content: `
applications:
  foo:
    relationships:
      database:
  app-with-dash:
    relationships:
      database:
  app_with_underscore:
    relationships:
      database:
  a:
    workers:
      worker-1:
        commands:
          start: echo ok
      worker_2:
        commands:
          start: echo ok
services:
  database:
    type: mariadb:11.4
  cache-service:
    type: valkey:8.0
  service_with_underscore:
    type: redis:7.2`,
		},
		{
			name: "reserved_name_router",
			content: `
services:
  router:
    type: mariadb:11.4`,
			expectErrorValues: []string{`services.router: "router" is a reserved name.`},
		},
		{
			name: "invalid_characters",
			content: `
applications:
  fooBar:
    relationships:
      database:
  foo.bar:
    relationships:
      database:
services:
  database:
    type: mariadb:11.4`,
			expectErrorValues: []string{
				`applications.fooBar: "fooBar" is not a valid application name, it can only contain lowercase alphanumeric characters, dashes, or underscores.`,   //nolint:lll
				`applications.foo.bar: "foo.bar" is not a valid application name, it can only contain lowercase alphanumeric characters, dashes, or underscores.`, //nolint:lll
			},
		},
		{
			name: "invalid_start_end",
			content: `
applications:
  _baz:
    relationships:
      database:
  foo-:
    relationships:
      database:
  -bar:
    relationships:
      database:
    workers:
      _badworker:
        commands:
          start: echo ok
services:
  database:
    type: mariadb:11.4`,
			expectErrorValues: []string{
				`applications._baz: "_baz" is not a valid application name, it should start and end with alphanumeric characters.`,
				`applications.foo-: "foo-" is not a valid application name, it should start and end with alphanumeric characters.`,
				`applications.-bar: "-bar" is not a valid application name, it should start and end with alphanumeric characters.`,
				`applications.-bar.workers._badworker: "_badworker" is not a valid worker name, it should start and end with alphanumeric characters.`, //nolint:lll
			},
		},
		{
			name: "double_dash",
			content: `
applications:
  foo--bar:
    relationships:
      database:
services:
  database:
    type: mariadb:11.4
  cache--service:
    type: valkey:8.0`,
			expectErrorValues: []string{
				`applications.foo--bar: "foo--bar" is not a valid application name, it should not contain double dashes.`,
				`services.cache--service: "cache--service" is not a valid service name, it should not contain double dashes.`,
			},
		},
		{
			name: "too_long",
			content: `
applications:
  averylongapplicationnamethatexceedsthirtytwocharacters:
    relationships:
      database:
services:
  database:
    type: mariadb:11.4
  averylongservicenamethatexceedsthirtytwocharacters:
    type: valkey:8.0`,
			expectErrorValues: []string{
				`applications.averylongapplicationnamethatexceedsthirtytwocharacters: "averylongapplicationnamethatexceedsthirtytwocharacters" is not a valid application name, it should be shorter than 32 characters.`, //nolint:lll
				`services.averylongservicenamethatexceedsthirtytwocharacters: "averylongservicenamethatexceedsthirtytwocharacters" is not a valid service name, it should be shorter than 32 characters.`,                 //nolint:lll
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := lint.DecodeConfig(c.content)
			if err != nil {
				assert.FailNow(t, "decodeConfig failed", err)
			}
			result := lint.CheckNames(cfg)
			if len(c.expectErrorValues) > 0 {
				assert.True(t, result.HasErrors())
				for _, v := range c.expectErrorValues {
					assert.ErrorContains(t, result, v)
				}
			} else {
				assert.False(t, result.HasErrors())
			}
		})
	}
}
