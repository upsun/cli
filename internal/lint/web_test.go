package lint_test

import (
	"testing"

	"github.com/upsun/cli/internal/lint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckWebConfig(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantErr  bool
		errMatch string
	}{
		{
			name: "location key without leading slash",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "api":
          root: "public"
`,
			wantErr:  true,
			errMatch: "linter errors:\n  - applications.app1.web.locations: location key \"api\" must start with a slash",
		},
		{
			name: "location key with empty segment",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/api//v1":
          root: "public"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations: location key \"/api//v1\" cannot include empty parts, '.' or '..'",
		},
		{
			name: "location key with dot segment",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/api/./v1":
          root: "public"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations: location key \"/api/./v1\" cannot include empty parts, '.' or '..'",
		},
		{
			name: "location key with dotdot segment",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/api/../v1":
          root: "public"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations: location key \"/api/../v1\" cannot include empty parts, '.' or '..'",
		},
		{
			name: "location key with regex pattern from issue AI-105",
			content: `
applications:
  testdemo:
    type: nodejs:24
    web:
      locations:
        "/":
          root: "public"
        "^/(api|health)":
          passthru: true
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.testdemo.web.locations: location key \"^/(api|health)\" must start with a slash",
		},
		{
			name: "location key with query string character",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/api?version=1":
          root: "public"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations: location key \"/api?version=1\" contains invalid characters",
		},
		{
			name: "location key with fragment character",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/page#section":
          root: "public"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations: location key \"/page#section\" contains invalid characters",
		},
		{
			name: "valid location keys",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "public"
        "/api":
          root: "api"
        "/api/v1":
          root: "api/v1"
        "/health-check":
          root: "health"
        "/static_files":
          root: "static"
        "/path.with.dots":
          root: "path"
        "/~user":
          root: "user"
        "/user@example":
          root: "user"
        "/path$end":
          root: "path"
        "/path(group)":
          root: "path"
        "/file+name":
          root: "file"
        "/trailing/":
          root: "trailing"
`,
		},
		{
			name: "invalid root with leading slash",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "/public"
`,
			wantErr:  true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root cannot begin with a slash",
		},
		{
			name: "invalid root with leading slash and trailing slash",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "/public/"
`,
			wantErr:  true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root cannot begin with a slash",
		},
		{
			name: "empty root",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: ""
`,
		},
		{
			name: "valid root without slash",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "public"
`,
		},
		{
			name: "invalid single slash root",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "/"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root cannot begin with a slash (use an empty string instead)",
		},
		{
			name: "invalid root with leading slash and empty part",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "/foo//bar"
`,
			wantErr:  true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root cannot begin with a slash",
		},
		{
			name: "invalid root with empty part (no leading slash)",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "foo//bar"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root path cannot include empty parts, '.' or '..'",
		},
		{
			name: "invalid root with leading slash and dot",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "/foo/./bar"
`,
			wantErr:  true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root cannot begin with a slash",
		},
		{
			name: "invalid root with dot (no leading slash)",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "foo/./bar"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root path cannot include empty parts, '.' or '..'",
		},
		{
			name: "invalid root with leading slash and dotdot",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "/foo/../bar"
`,
			wantErr:  true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root cannot begin with a slash",
		},
		{
			name: "invalid root with dotdot (no leading slash)",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "foo/../bar"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root path cannot include empty parts, '.' or '..'",
		},
		{
			name: "multiple failing locations with leading slashes",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/foo":
          root: "/foo/./bar"
        "/bar":
          root: "/bar//baz"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/bar\"].root: the root cannot begin with a slash\n" +
				"  - applications.app1.web.locations[\"/foo\"].root: the root cannot begin with a slash",
		},
		{
			name: "multiple failing apps with leading slashes",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/foo":
          root: "/foo/./bar"
  app2:
    type: php:8.4
    web:
      locations:
        "/bar":
          root: "/bar//baz"
`,
			wantErr: true,
			//nolint:lll
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/foo\"].root: the root cannot begin with a slash\n" +
				"  - applications.app2.web.locations[\"/bar\"].root: the root cannot begin with a slash",
		},
		{
			name: "valid regex rules",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "public"
          rules:
            "^/api/":
              allow: false
            "\\.css$":
              expires: "1d"
            "^/images/.*\\.(jpg|png|gif)$":
              cache: true
`,
		},
		{
			name: "invalid regex rule - unmatched parentheses",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "public"
          rules:
            "^/api/(unclosed":
              allow: false
`,
			wantErr: true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].rules: " +
				"invalid regular expression: error parsing regexp: missing closing ) in `^/api/(unclosed`",
		},
		{
			name: "invalid regex rule - invalid escape sequence",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "public"
          rules:
            "\\x":
              allow: false
`,
			wantErr: true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].rules: " +
				"invalid regular expression: error parsing regexp: insufficient hexadecimal digits in `\\x`",
		},
		{
			name: "empty regex rule pattern",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "public"
          rules:
            "":
              allow: false
`,
			wantErr:  true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].rules: rule pattern cannot be empty",
		},
		{
			name: "multiple invalid regex rules",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "public"
          rules:
            "^/api/(unclosed":
              allow: false
            "[invalid":
              cache: true
`,
			wantErr: true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].rules: " +
				"invalid regular expression: error parsing regexp: missing closing ) in `^/api/(unclosed`\n" +
				"  - applications.app1.web.locations[\"/\"].rules: " +
				"invalid regular expression: error parsing regexp: unterminated [] set in `[invalid`",
		},
		{
			name: "mixed root and regex linter errors",
			content: `
applications:
  app1:
    type: php:8.4
    web:
      locations:
        "/":
          root: "/foo/../bar"
          rules:
            "^/api/(unclosed":
              allow: false
`,
			wantErr:  true,
			errMatch: "linter errors:\n  - applications.app1.web.locations[\"/\"].root: the root cannot begin with a slash",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := lint.DecodeConfig(c.content)
			assert.NoError(t, err)
			result := lint.CheckWebConfig(cfg)
			if c.wantErr {
				require.True(t, result.HasErrors())
				assert.Equal(t, c.errMatch, result.Error())
			} else {
				assert.False(t, result.HasErrors())
			}
		})
	}
}
