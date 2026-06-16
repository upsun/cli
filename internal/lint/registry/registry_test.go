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
	// Don't assert a specific version, which drifts as the registry is refreshed.
	assert.NotEmpty(t, reg["golang"].Versions.Supported)
	assert.True(t, reg["golang"].IsRuntime)
	// redis-persistent is added by clean().
	assert.Contains(t, reg, "redis-persistent")
}
