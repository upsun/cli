package tests

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHiddenAliases(t *testing.T) {
	f := newCommandFactory(t, "", "")

	cases := []struct {
		alias   string
		command string
	}{
		{"snapshots", "backup:list"},
		{"logs", "environment:logs"},
		{"user:role", "user:get"},
		{"environment:sql", "db:sql"},
	}
	for _, c := range cases {
		t.Run(c.alias, func(t *testing.T) {
			var data struct {
				Name string `json:"name"`
			}
			require.NoError(t, json.Unmarshal([]byte(f.Run("help", c.alias, "--format", "json")), &data))
			assert.Equal(t, c.command, data.Name)
		})
	}

	help := f.Run("help", "snapshots")
	assert.Contains(t, help, "Aliases: backups")
	assert.NotContains(t, help, "snapshot")

	assert.NotContains(t, f.Run("list", "backup"), "snapshot")
}
