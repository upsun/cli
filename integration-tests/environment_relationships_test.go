package tests

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelationshipsLocal(t *testing.T) {
	relationships := map[string]any{
		"database": []any{
			map[string]any{
				"service":  "db",
				"host":     "database.internal",
				"rel":      "mysql",
				"scheme":   "mysql",
				"username": "user",
				"password": "",
				"path":     "main",
				"port":     3306,
				"type":     "mysql:10.6",
			},
		},
		"redis": []any{
			map[string]any{
				"service":  "cache",
				"host":     "redis.internal",
				"rel":      "redis",
				"scheme":   "redis",
				"username": "",
				"password": "",
				"path":     "",
				"port":     6379,
				"type":     "redis:7.0",
			},
		},
	}

	data, err := json.Marshal(relationships)
	require.NoError(t, err)

	f := &cmdFactory{t: t}
	f.extraEnv = []string{"PLATFORM_RELATIONSHIPS=" + base64.StdEncoding.EncodeToString(data)}

	// List all relationships.
	output := f.Run("environment:relationships")
	assert.Contains(t, output, "database")
	assert.Contains(t, output, "redis")

	// Extract a specific property (fully-qualified path: relationship.index.key).
	assertTrimmed(t, "database.internal", f.Run("environment:relationships", "-P", "database.0.host"))
	assertTrimmed(t, "redis.internal", f.Run("environment:relationships", "-P", "redis.0.host"))
	assertTrimmed(t, "3306", f.Run("environment:relationships", "-P", "database.0.port"))
}
