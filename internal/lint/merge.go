package lint

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// findUpsunConfigFiles returns all .upsun/*.yaml and .upsun/*.yml files in the given directory.
func findUpsunConfigFiles(fsys fs.FS, path string) ([]string, error) {
	// Find both .yaml and .yml files
	patterns := []string{
		filepath.Join(path, ".upsun", "*.yaml"),
		filepath.Join(path, ".upsun", "*.yml"),
	}

	var allMatches []string
	for _, pattern := range patterns {
		matches, err := fs.Glob(fsys, pattern)
		if err != nil {
			return nil, fmt.Errorf("could not glob .upsun directory: %w", err)
		}
		allMatches = append(allMatches, matches...)
	}

	if len(allMatches) == 0 {
		return nil, fmt.Errorf("no configuration files found matching %s or %s", patterns[0], patterns[1])
	}
	return allMatches, nil
}

// mergeConfigFiles merges the given YAML files, combining top-level 'applications', 'routes', and 'services' maps.
// If a key is duplicated across files, it returns an error. Returns the merged YAML as a string.
func mergeConfigFiles(fsys fs.FS, files []string) (string, error) {
	merged := map[string]map[string]any{
		"applications": {},
		"routes":       {},
		"services":     {},
	}
	for _, file := range files {
		b, err := fs.ReadFile(fsys, file)
		if err != nil {
			return "", fmt.Errorf("failed to read %s: %w", file, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return "", fmt.Errorf("failed to parse YAML in %s: %w", file, err)
		}
		for _, key := range []string{"applications", "routes", "services"} {
			if section, ok := doc[key]; ok && section != nil {
				sectionMap, ok := section.(map[string]any)
				if !ok {
					return "", fmt.Errorf("%s in %s is not a map", key, file)
				}
				for k, v := range sectionMap {
					if _, exists := merged[key][k]; exists {
						return "", fmt.Errorf("duplicate key '%s' in section '%s' found in file %s", k, key, file)
					}
					merged[key][k] = v
				}
			}
		}
	}
	out := map[string]any{}
	for _, key := range []string{"applications", "routes", "services"} {
		if len(merged[key]) > 0 {
			out[key] = merged[key]
		}
	}
	buf, err := yaml.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged YAML: %w", err)
	}
	return string(buf), nil
}

// getMergedConfigFiles merges all .upsun/*.yaml files in the given directory.
// It is a convenience wrapper for findUpsunConfigFiles + mergeConfigFiles.
func getMergedConfigFiles(fsys fs.FS, path string) (string, error) {
	files, err := findUpsunConfigFiles(fsys, path)
	if err != nil {
		return "", err
	}
	return mergeConfigFiles(fsys, files)
}
