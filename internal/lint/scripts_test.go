package lint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/lint"
)

func TestCheckScripts(t *testing.T) {
	tests := []struct {
		name                 string
		yamlContent          string
		expectErrorMessage   string
		expectWarningMessage string
	}{
		{
			name: "all valid scripts",
			yamlContent: `
applications:
  app1:
    type: "nodejs:20"
    hooks:
      build: "echo 'Building app1'"
      deploy: "cp -r ./build /var/www"
      post_deploy: "chmod -R 755 /var/www"
    web:
      commands:
        start: "node server.js"
        post_start: "echo 'Server started'"
`,
		},
		{
			name: "invalid build script",
			yamlContent: `
applications:
  app1:
    type: "nodejs:20"
    hooks:
      build: "echo 'Missing quote"
      deploy: "cp -r ./build /var/www"
    web:
      commands:
        start: "node server.js"
`,
			expectErrorMessage: "linter errors:\n  - applications.app1.hooks.build: invalid syntax: 1:6: " +
				"reached EOF without closing quote `'`",
		},
		{
			name: "invalid deploy script with unmatched parenthesis",
			yamlContent: `
applications:
  app1:
    type: "php:8.2"
    hooks:
      build: "echo 'Building app1'"
      deploy: "if (true; then echo 'deployed'; fi"
`,
			expectErrorMessage: "linter errors:\n  - applications.app1.hooks.deploy: invalid syntax: 1:11: " +
				"`then` can only be used in an `if`",
		},
		{
			name: "invalid start command with pipe error",
			yamlContent: `
applications:
  app1:
    type: "nodejs:20"
    web:
      commands:
        start: "node server.js | | grep output"
`,
			expectErrorMessage: "linter errors:\n  - applications.app1.web.commands.start: invalid syntax: 1:16: " +
				"`|` must be followed by a statement",
		},
		{
			name: "multiple applications with one invalid script",
			yamlContent: `
applications:
  app1:
    type: "nodejs:20"
    hooks:
      build: "echo 'Building app1'"
    web:
      commands:
        start: "node server.js"
  app2:
    type: "nodejs:20"
    hooks:
      build: "for ((i=0; i<5; i++)) do echo $i; done"  # bash syntax, not POSIX
    web:
      commands:
        start: "python app.py"
`,
			expectErrorMessage: "linter errors:\n  - applications.app2.hooks.build: invalid syntax: 1:5: " +
				"c-style fors are a bash/zsh feature; tried parsing as posix",
		},
		{
			name: "empty YAML",
			yamlContent: `
applications: {}`,
		},
		{
			name:               "invalid YAML",
			yamlContent:        `not valid yaml: ]`,
			expectErrorMessage: "failed to parse YAML: yaml: did not find expected node content",
		},
		{
			name: "missing start command for non-PHP application",
			yamlContent: `
applications:
  app1:
    type: "nodejs:20"
    hooks:
      build: "echo 'Building app1'"
`,
			expectWarningMessage: "linter warnings:\n  - applications.app1.web.commands.start: " +
				"a start command is needed for non-PHP applications",
		},
		{
			name: "PHP application without start command - no warning",
			yamlContent: `
applications:
  app1:
    type: "php:8.2"
    hooks:
      build: "echo 'Building PHP app'"
`,
		},
		{
			name: "composable application without start command - no warning",
			yamlContent: `
applications:
  app1:
    type: "composable:nginx"
    hooks:
      build: "echo 'Building composable app'"
`,
		},
		{
			name: "non-PHP application with start command - no warning",
			yamlContent: `
applications:
  app1:
    type: "nodejs:20"
    hooks:
      build: "echo 'Building app1'"
    web:
      commands:
        start: "node server.js"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := lint.DecodeConfig(tt.yamlContent)
			if err != nil {
				assert.Equal(t, tt.expectErrorMessage, err.Error())
				return
			}
			result := lint.CheckScripts(cfg)

			if tt.expectErrorMessage != "" {
				assert.True(t, result.HasErrors())
				assert.Equal(t, tt.expectErrorMessage, result.Error())
			} else {
				assert.False(t, result.HasErrors())
			}

			if tt.expectWarningMessage != "" {
				assert.True(t, result.HasWarnings())
				assert.Equal(t, tt.expectWarningMessage, result.Error())
			} else {
				assert.False(t, result.HasWarnings())
			}
		})
	}
}
