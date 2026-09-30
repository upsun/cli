package lint

import (
	"sort"
	"testing"
)

func TestCheckDependencies(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantErrs []string
	}{
		{
			name: "valid dependencies",
			yaml: `
applications:
  app:
    type: nodejs:22
    dependencies:
      nodejs:
        sharp: '*'
        lodash: '^4.17.21'
      php:
        guzzlehttp/guzzle: '^7.0'
      python3:
        requests: '2.28.1'
      ruby:
        nokogiri: '1.13.8'
`,
		},
		{
			name: "invalid dependency type",
			yaml: `
applications:
  app:
    type: nodejs:22
    dependencies:
      golang:
        gin: 'v1.9.1'
`,
			wantErrs: []string{
				"applications.app.dependencies.golang: invalid dependency type 'golang'; must be one of: nodejs, php, python3, ruby", //nolint:lll
			},
		},
		{
			name: "multiple invalid dependency types",
			yaml: `
applications:
  app:
    type: nodejs:22
    dependencies:
      golang:
        gin: 'v1.9.1'
      java:
        spring: '2.7.0'
`,
			wantErrs: []string{
				"applications.app.dependencies.golang: invalid dependency type 'golang'; must be one of: nodejs, php, python3, ruby", //nolint:lll
				"applications.app.dependencies.java: invalid dependency type 'java'; must be one of: nodejs, php, python3, ruby",
			},
		},
		{
			name: "empty package version",
			yaml: `
applications:
  app:
    type: nodejs:22
    dependencies:
      nodejs:
        sharp: ''
`,
			wantErrs: []string{
				"applications.app.dependencies.nodejs.sharp: package version cannot be empty",
			},
		},
		{
			name: "no dependencies section",
			yaml: `
applications:
  app:
    type: nodejs:22
`,
		},
		{
			name: "php custom repositories",
			yaml: `
applications:
  app:
    type: php:8.3
    # Example from: https://docs.upsun.com/languages/php.html#alternative-repositories
    dependencies:
      php:
        require:
          "platformsh/client": "2.x-dev"
        repositories:
          - type: vcs
            url: "git@github.com:platformsh/platformsh-client-php.git"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := DecodeConfig(tt.yaml)
			if err != nil {
				t.Fatalf("failed to decode YAML: %v", err)
			}

			result := CheckDependencies(cfg)

			if len(tt.wantErrs) == 0 {
				if result.HasErrors() {
					t.Errorf("expected no errors, got: %v", result.Errors)
				}
				return
			}

			if !result.HasErrors() {
				t.Errorf("expected errors, got none")
				return
			}

			if len(result.Errors) != len(tt.wantErrs) {
				t.Errorf("expected %d errors, got %d: %v", len(tt.wantErrs), len(result.Errors), result.Errors)
				return
			}

			// Convert errors to strings and sort both expected and got for comparison
			var gotErrs []string
			for _, err := range result.Errors {
				gotErrs = append(gotErrs, err.Path+": "+err.Message)
			}
			sort.Strings(gotErrs)

			wantErrsSorted := make([]string, len(tt.wantErrs))
			copy(wantErrsSorted, tt.wantErrs)
			sort.Strings(wantErrsSorted)

			for i, wantErr := range wantErrsSorted {
				if gotErrs[i] != wantErr {
					t.Errorf("error %d: expected %q, got %q", i, wantErr, gotErrs[i])
				}
			}
		})
	}
}
