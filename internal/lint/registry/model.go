package registry

import (
	"encoding/json"
	"slices"
)

type Registry map[string]Image

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

	Description   string `json:"description,omitempty" yaml:"description,omitempty"`
	Configuration string `json:"configuration,omitempty" yaml:"configuration,omitempty"`
	Docs          Docs   `json:"docs,omitzero" yaml:"docs,omitempty"`
}

// Docs contains documentation for the image.
type Docs struct {
	RelationshipName *string               `json:"relationship_name,omitempty" yaml:"relationship_name,omitempty"`
	ServiceName      *string               `json:"service_name,omitempty" yaml:"service_name,omitempty"`
	URL              string                `json:"url,omitempty" yaml:"url,omitempty"`
	Web              Web                   `json:"web,omitzero" yaml:"web,omitempty"`
	Hooks            Hooks                 `json:"hooks,omitzero" yaml:"hooks,omitempty"`
	Build            BuildConfig           `json:"build,omitzero" yaml:"build,omitempty"`
	Dependencies     map[string]Dependency `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
}

// Web describes web server configuration for runtime images.
type Web struct {
	Commands struct {
		Start string `json:"start,omitempty" yaml:"start,omitempty"`
	} `json:"commands,omitzero" yaml:"commands,omitempty"`
	Locations map[string]Location `json:"locations,omitempty" yaml:"locations,omitempty"`
	Upstream  Upstream            `json:"upstream,omitzero" yaml:"upstream,omitempty"`
}

// Upstream config for protocols/sockets.
type Upstream struct {
	SocketFamily string `json:"socket_family,omitempty" yaml:"socket_family,omitempty"`
	Protocol     string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

// Location defines routing rules inside the Web configuration.
type Location struct {
	Root     string `json:"root,omitempty" yaml:"root,omitempty"`
	Allow    bool   `json:"allow,omitempty" yaml:"allow,omitempty"`
	Passthru any    `json:"passthru,omitempty" yaml:"passthru,omitempty"` // String or bool
	Expires  any    `json:"expires,omitempty" yaml:"expires,omitempty"`   // String or int
}

// Hooks define build and deploy lifecycle commands.
type Hooks struct {
	Build  any `json:"build,omitempty" yaml:"build,omitempty"` // String or list?
	Deploy any `json:"deploy,omitempty" yaml:"deploy,omitempty"`
}

// BuildConfig holds custom build settings.
type BuildConfig struct {
	Flavor string `json:"flavor,omitempty" yaml:"flavor,omitempty"`
}

// Dependency version map under Docs.
type Dependency map[string]string

// VersionInfo lists deprecated, supported, and optional legacy versions.
type VersionInfo struct {
	Supported []string `json:"supported"`

	Deprecated []string `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Legacy     []string `json:"legacy,omitempty" yaml:"legacy,omitempty"`
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

// LatestVersion returns the most recent supported version.
// Returns empty string if no supported versions exist.
func (v VersionInfo) LatestVersion() string {
	if len(v.Supported) == 0 {
		return ""
	}
	return v.Supported[0]
}
