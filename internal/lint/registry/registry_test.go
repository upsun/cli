package registry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/internal/lint/registry"
)

// Basic test to ensure that the registry can be parsed.
func TestRegistry(t *testing.T) {
	reg, err := registry.Parsed()
	assert.NoError(t, err)
	assert.NotEmpty(t, reg)
	assert.Contains(t, reg, "golang")
	assert.Contains(t, reg["golang"].Versions.Supported, "1.24")
	assert.Contains(t, reg, "redis-persistent")
}
