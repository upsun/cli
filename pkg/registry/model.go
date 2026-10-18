package registry

import (
	"encoding/json"
	"slices"
)

// Registry maps image types (e.g. "php", "postgresql") to their images.
type Registry map[string]Image

// AllTypes returns the sorted image types: runtimes if runtime is true, otherwise services.
func (r Registry) AllTypes(runtime bool) []string {
	types := make([]string, 0, len(r))
	for k := range r {
		if r[k].IsRuntime == runtime {
			types = append(types, k)
		}
	}
	slices.Sort(types)
	return types
}

// Image describes a single service/runtime container image.
type Image struct {
	Name      string      `json:"name" yaml:"name,omitempty"`
	Type      string      `json:"type" yaml:"type"`
	IsRuntime bool        `json:"runtime" yaml:"is_runtime"`
	Versions  VersionInfo `json:"versions,omitzero" yaml:"versions,omitempty"`
}

// VersionInfo lists deprecated, supported, and optional legacy and retired versions.
type VersionInfo struct {
	Supported []string `json:"supported"`

	Deprecated []string `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Legacy     []string `json:"legacy,omitempty" yaml:"legacy,omitempty"`
	Retired    []string `json:"retired,omitempty" yaml:"retired,omitempty"`
}

// UnmarshalJSON handles both string and object formats for versions field.
// Some registry entries have versions as a string (e.g. "25.05") instead of an object.
func (v *VersionInfo) UnmarshalJSON(data []byte) error {
	// Try unmarshaling as a string first.
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		// It's a string, treat it as a single supported version.
		v.Supported = []string{str}
		return nil
	}

	// Try unmarshaling as the normal object structure.
	type versionInfoAlias VersionInfo
	var obj versionInfoAlias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*v = VersionInfo(obj)
	return nil
}
