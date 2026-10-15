package schema

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

func TestLoadValidatesConfig(t *testing.T) {
	s, err := Load()
	require.NoError(t, err, "Failed to load schema")

	testYAML := `
applications:
  myapp:
    type: nodejs:22
`
	var data = make(map[string]any)
	require.NoError(t, yaml.Unmarshal([]byte(testYAML), &data))

	result, err := s.Validate(gojsonschema.NewGoLoader(data))
	require.NoError(t, err)
	assert.True(t, result.Valid(), "Schema should accept a basic config: %v", result.Errors())
}

// TestSchemaDefaults checks that each default is valid, as editors insert defaults on completion.
func TestSchemaDefaults(t *testing.T) {
	var root map[string]any
	require.NoError(t, json.Unmarshal(configSchema, &root))

	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch n := node.(type) {
		case map[string]any:
			if def, ok := n["default"]; ok {
				sub := make(map[string]any, len(n)+1)
				for k, v := range n {
					sub[k] = v
				}
				sub["definitions"] = root["definitions"]
				sub["$schema"] = root["$schema"]
				s, err := gojsonschema.NewSchema(gojsonschema.NewGoLoader(sub))
				require.NoError(t, err, path)
				result, err := s.Validate(gojsonschema.NewGoLoader(def))
				require.NoError(t, err, path)
				assert.True(t, result.Valid(), "%s: invalid default %v: %v", path, def, result.Errors())
			}
			for k, v := range n {
				if d, ok := v.(string); ok && k == "description" {
					assert.NotContains(t, d, "\t", "%s: tab in description", path)
				}
				walk(v, path+"."+k)
			}
		case []any:
			for i, v := range n {
				walk(v, fmt.Sprintf("%s.%d", path, i))
			}
		}
	}
	walk(root, "")
}
