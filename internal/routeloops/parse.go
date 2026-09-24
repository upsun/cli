package routeloops

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// routeSpec captures the fields we care about from a routes.yaml entry.
type routeSpec struct {
	Type     string `yaml:"type"`
	To       string `yaml:"to"`
	Upstream string `yaml:"upstream"`
}

// ParseRoutesYAML decodes a top-level `URL: {type, to, upstream}` map, matching
// the shape of .platform/routes.yaml.
func ParseRoutesYAML(data []byte) ([]Route, error) {
	var m map[string]routeSpec
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse routes yaml: %w", err)
	}
	return mapToRoutes(m), nil
}

// ParseUpsunConfig decodes a config file that has a top-level `routes:` map.
// Returns an empty slice (nil error) if the file has no routes section.
func ParseUpsunConfig(data []byte) ([]Route, error) {
	var doc struct {
		Routes map[string]routeSpec `yaml:"routes"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse upsun config: %w", err)
	}
	return mapToRoutes(doc.Routes), nil
}

// ParseFile reads a YAML file and picks the right shape: if it has a top-level
// `routes:` key it's treated as an Upsun-style config, otherwise as a
// routes.yaml.
func ParseFile(path string) ([]Route, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the CLI user
	if err != nil {
		return nil, err
	}
	routes, err := parseAuto(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return routes, nil
}

func parseAuto(data []byte) ([]Route, error) {
	var probe map[string]yaml.Node
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if _, hasRoutes := probe["routes"]; hasRoutes {
		return ParseUpsunConfig(data)
	}
	return ParseRoutesYAML(data)
}

// ParseLiveCSV decodes CSV rows from `route:list --format=csv --columns=route,type,to --no-header`.
// The `to` column is already flattened server-side: for upstream rows it holds
// the upstream name, for redirect rows it holds the redirect target. We only
// populate Route.To when Type == "redirect" to keep semantics consistent with
// the YAML parsers.
func ParseLiveCSV(data []byte) ([]Route, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	var out []Route
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse route:list csv: %w", err)
		}
		if len(rec) < 2 {
			continue
		}
		route := Route{URL: rec[0], Type: rec[1]}
		if len(rec) >= 3 && route.Type == TypeRedirect {
			route.To = rec[2]
		}
		if route.URL == "" || route.Type == "" {
			continue
		}
		out = append(out, route)
	}
	return out, nil
}

// DiscoverProjectRoutes looks for a routes configuration under dir. It prefers
// .platform/routes.yaml; failing that it walks .upsun/**/*.{yaml,yml} and
// unions any file whose top-level has a `routes:` key. Returns the routes and
// a short human-readable description of the source path(s).
func DiscoverProjectRoutes(dir string) ([]Route, string, error) {
	platformPath := filepath.Join(dir, ".platform", "routes.yaml")
	if info, err := os.Stat(platformPath); err == nil && !info.IsDir() {
		routes, err := ParseFile(platformPath)
		if err != nil {
			return nil, "", err
		}
		return routes, platformPath, nil
	}

	upsunDir := filepath.Join(dir, ".upsun")
	if info, err := os.Stat(upsunDir); err == nil && info.IsDir() {
		routes, sources, err := readUpsunRoutes(upsunDir)
		if err != nil {
			return nil, "", err
		}
		if len(sources) > 0 {
			return routes, strings.Join(sources, ", "), nil
		}
	}

	return nil, "", fmt.Errorf(
		"no routes configuration found under %s (looked for .platform/routes.yaml and .upsun/*.yaml with a `routes:` key)",
		dir,
	)
}

func readUpsunRoutes(upsunDir string) ([]Route, []string, error) {
	var files []string
	err := filepath.WalkDir(upsunDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)

	seen := make(map[string]struct{})
	var all []Route
	var sources []string
	for _, path := range files {
		data, err := os.ReadFile(path) //nolint:gosec // path comes from a project dir walk
		if err != nil {
			return nil, nil, err
		}
		var probe map[string]yaml.Node
		if err := yaml.Unmarshal(data, &probe); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if _, hasRoutes := probe["routes"]; !hasRoutes {
			continue
		}
		routes, err := ParseUpsunConfig(data)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, r := range routes {
			if _, dup := seen[r.URL]; dup {
				continue
			}
			seen[r.URL] = struct{}{}
			all = append(all, r)
		}
		sources = append(sources, path)
	}
	return all, sources, nil
}

func mapToRoutes(m map[string]routeSpec) []Route {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Route, 0, len(m))
	for _, k := range keys {
		spec := m[k]
		if spec.Type != TypeUpstream && spec.Type != TypeRedirect {
			continue
		}
		out = append(out, Route{URL: k, Type: spec.Type, To: spec.To})
	}
	return out
}
