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

	// Namespaces only used by hidden aliases are not listed.
	_, stdErr, err := f.RunCombinedOutput("snapshot")
	assert.Error(t, err)
	assert.Contains(t, stdErr, `Command "snapshot" is not defined.`)

	var list struct {
		Commands []struct {
			Name          string   `json:"name"`
			HiddenAliases []string `json:"hidden_aliases"`
		} `json:"commands"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.Run("list", "--format", "json")), &list))
	var found bool
	for _, c := range list.Commands {
		if c.Name == "backup:list" {
			found = true
			assert.Equal(t, []string{"snapshots", "snapshot:list"}, c.HiddenAliases)
		}
	}
	assert.True(t, found)
}
