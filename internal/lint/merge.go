package lint

import (
	"fmt"
	"io/fs"
	"path"

	"gopkg.in/yaml.v3"
)

// findFlexConfigFiles returns all *.yaml and *.yml files in the Flex config
// directory (e.g. .upsun) of the given directory. Flex files may have any name.
func findFlexConfigFiles(fsys fs.FS, dir, configDir string) ([]string, error) {
	// io/fs paths are always slash-separated, so path.Join is used here rather
	// than filepath.Join, whose Windows separator would match nothing.
	patterns := []string{
		path.Join(dir, configDir, "*.yaml"),
		path.Join(dir, configDir, "*.yml"),
	}

	var allMatches []string
	for _, pattern := range patterns {
		matches, err := fs.Glob(fsys, pattern)
		if err != nil {
			return nil, fmt.Errorf("could not glob %s directory: %w", configDir, err)
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
		keyApplications: {},
		keyRoutes:       {},
		keyServices:     {},
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
		for _, key := range []string{keyApplications, keyRoutes, keyServices} {
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
	for _, key := range []string{keyApplications, keyRoutes, keyServices} {
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

// getMergedConfigFiles merges all Flex config files in the given directory.
// It is a convenience wrapper for findFlexConfigFiles + mergeConfigFiles.
func getMergedConfigFiles(fsys fs.FS, dir, configDir string) (string, error) {
	files, err := findFlexConfigFiles(fsys, dir, configDir)
	if err != nil {
		return "", err
	}
	return mergeConfigFiles(fsys, files)
}
