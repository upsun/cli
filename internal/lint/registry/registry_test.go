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

// The persistent variants are aliases added by clean(), tracking the versions
// of the type they copy.
func TestRegistry_PersistentAliases(t *testing.T) {
	reg, err := registry.Parsed()
	assert.NoError(t, err)

	for _, c := range []struct{ base, alias string }{
		{"redis", "redis-persistent"},
		{"valkey", "valkey-persistent"},
	} {
		t.Run(c.alias, func(t *testing.T) {
			assert.Contains(t, reg, c.alias)
			assert.NotEmpty(t, reg[c.alias].Versions.Supported)
			assert.Equal(t, reg[c.base].Versions, reg[c.alias].Versions)
		})
	}
}

// Replica images are listed upstream without versions of their own, so clean()
// takes them from the base type. Without this they reject every version.
func TestRegistry_ReplicaVersions(t *testing.T) {
	reg, err := registry.Parsed()
	assert.NoError(t, err)

	for _, base := range []string{"mariadb", "postgresql"} {
		t.Run(base+"-replica", func(t *testing.T) {
			replica := base + "-replica"
			assert.Contains(t, reg, replica)
			assert.NotEmpty(t, reg[replica].Versions.Supported)
			assert.Equal(t, reg[base].Versions, reg[replica].Versions)
		})
	}
}
