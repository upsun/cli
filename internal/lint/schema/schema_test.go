package schema

import (
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
