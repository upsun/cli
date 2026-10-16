package schema

import (
	"encoding/json"
	"fmt"
	"strings"
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
				s, err := compile(gojsonschema.NewGoLoader(sub))
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

// TestSchemaKeywords checks that no keyword is beside a $ref, that remote refs
// are replaced, and that mappings with fixed keys allow keys starting with ".",
// which the platform ignores (e.g. YAML anchors), except in the stack, which the
// platform checks itself.
func TestSchemaKeywords(t *testing.T) {
	var root map[string]any
	require.NoError(t, json.Unmarshal(configSchema, &root))

	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch n := node.(type) {
		case map[string]any:
			if ref, ok := n["$ref"].(string); ok {
				assert.Len(t, n, 1, "%s: draft-07 ignores keywords beside $ref", path)
				if !strings.HasPrefix(ref, "#") {
					assert.Contains(t, remoteRefs, ref, "%s: remote $ref must be in remoteRefs, or it is fetched", path)
				}
			}
			if n["additionalProperties"] == false && path != ".definitions.stack" {
				assert.Equal(t, map[string]any{"^\\.": map[string]any{}}, n["patternProperties"], path)
			}
			for k, v := range n {
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
