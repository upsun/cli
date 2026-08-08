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

func TestVersionInfo_LatestVersion(t *testing.T) {
	cases := []struct {
		name     string
		versions registry.VersionInfo
		expected string
	}{
		{
			name: "returns_first_supported_version",
			versions: registry.VersionInfo{
				Supported: []string{"8.4", "8.3", "8.2"},
			},
			expected: "8.4",
		},
		{
			name: "returns_single_version",
			versions: registry.VersionInfo{
				Supported: []string{"22"},
			},
			expected: "22",
		},
		{
			name: "returns_empty_when_no_versions",
			versions: registry.VersionInfo{
				Supported: []string{},
			},
			expected: "",
		},
		{
			name: "returns_empty_when_nil",
			versions: registry.VersionInfo{
				Supported: nil,
			},
			expected: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := c.versions.LatestVersion()
			assert.Equal(t, c.expected, result)
		})
	}
}

func TestVersionInfo_LatestVersion_WithRealRegistry(t *testing.T) {
	reg, err := registry.Parsed()
	assert.NoError(t, err)
	assert.NotEmpty(t, reg)

	// Test with real registry data.
	if img, ok := reg["php"]; ok {
		latest := img.Versions.LatestVersion()
		assert.NotEmpty(t, latest, "PHP should have a latest version")
		assert.Contains(t, img.Versions.Supported, latest, "Latest version should be in supported list")
		if len(img.Versions.Supported) > 0 {
			assert.Equal(t, img.Versions.Supported[0], latest, "Latest version should be first in supported list")
		}
	}

	if img, ok := reg["postgresql"]; ok {
		latest := img.Versions.LatestVersion()
		assert.NotEmpty(t, latest, "PostgreSQL should have a latest version")
	}
}
