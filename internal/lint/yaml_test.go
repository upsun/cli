package lint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xeipuuv/gojsonschema"

	"github.com/upsun/cli/internal/lint"
)

func TestCheckYAMLSchema(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantErr  bool
		errorMsg string
	}{
		{
			name: "valid YAML with schema compliance",
			content: `
key: value
list:
  - item1
  - item2`,
			wantErr: false,
		},
		{
			name:     "valid YAML but not matching schema",
			content:  `invalidKey: someValue`,
			wantErr:  true,
			errorMsg: "linter errors:",
		},
		{
			name: "invalid YAML malformed syntax",
			content: `
key: value
list:
  - item1
  - item2
  [misplacedItem]`,
			wantErr:  true,
			errorMsg: "yaml: line 6: could not find expected ':'",
		},
		{
			name:     "empty YAML content",
			wantErr:  true,
			errorMsg: "linter errors:",
		},
		{
			name: "YAML with invalid types against schema",
			content: `
key: 12345
list:
  - item1
  - item2`,
			wantErr:  true,
			errorMsg: "linter errors:",
		},
	}

	schema := mockSchema()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := lint.CheckYAMLSchema(c.content, schema)
			if c.wantErr {
				assert.True(t, result.HasErrors())
				if c.errorMsg != "" {
					assert.Contains(t, result.Error(), c.errorMsg)
				}
			} else {
				assert.False(t, result.HasErrors())
			}
		})
	}
}

func mockSchema() *gojsonschema.Schema {
	schema, _ := gojsonschema.NewSchema(gojsonschema.NewStringLoader(`
	{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type": "object",
		"properties": {
			"key": {
				"type": "string"
			},
			"list": {
				"type": "array",
				"items": {
					"type": "string"
				}
			}
		},
		"required": ["key"]
	}`))
	return schema
}
