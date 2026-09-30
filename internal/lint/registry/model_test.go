package registry_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/lint/registry"
)

func TestVersionInfo_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		name     string
		json     string
		expected registry.VersionInfo
	}{
		{
			name: "object_with_supported_versions",
			json: `{"supported": ["8.4", "8.3"], "deprecated": ["8.0"]}`,
			expected: registry.VersionInfo{
				Supported:  []string{"8.4", "8.3"},
				Deprecated: []string{"8.0"},
			},
		},
		{
			name: "string_version",
			json: fmt.Sprintf(`%q`, registry.ChannelStable),
			expected: registry.VersionInfo{
				Supported: []string{registry.ChannelStable},
			},
		},
		{
			name: "empty_object",
			json: `{}`,
			expected: registry.VersionInfo{
				Supported: nil,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var v registry.VersionInfo
			err := json.Unmarshal([]byte(c.json), &v)
			require.NoError(t, err)
			assert.Equal(t, c.expected, v)
		})
	}
}
