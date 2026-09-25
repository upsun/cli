package lint

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

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

// mergeConfigFiles merges the given YAML files, combining the top-level sections
// (flexTopKeys). A key duplicated across files, or an unknown top-level key, is
// an error. It returns the merged YAML and the file that defines each entry,
// keyed by "<section>.<name>" (e.g. "applications.app").
func mergeConfigFiles(fsys fs.FS, files []string) (merged string, sources map[string]string, err error) {
	sections := map[string]map[string]any{}
	sources = map[string]string{}
	for _, file := range files {
		b, err := fs.ReadFile(fsys, file)
		if err != nil {
			return "", nil, fmt.Errorf("failed to read %s: %w", file, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return "", nil, fmt.Errorf("failed to parse YAML in %s: %w", file, err)
		}
		for key, section := range doc {
			if strings.HasPrefix(key, ".") {
				continue
			}
			if !slices.Contains(flexTopKeys, key) {
				return "", nil, fmt.Errorf("unknown top-level key '%s' in %s: it must be one of: %s",
					key, file, strings.Join(flexTopKeys, ", "))
			}
			if section == nil {
				continue
			}
			sectionMap, ok := section.(map[string]any)
			if !ok {
				return "", nil, fmt.Errorf("%s in %s is not a map", key, file)
			}
			if sections[key] == nil {
				sections[key] = map[string]any{}
			}
			for k, v := range sectionMap {
				if _, exists := sections[key][k]; exists {
					return "", nil, fmt.Errorf("duplicate key '%s' in section '%s' found in file %s (already defined in %s)",
						k, key, file, sources[key+"."+k])
				}
				sections[key][k] = v
				sources[key+"."+k] = file
			}
		}
	}
	out := map[string]any{}
	for key, section := range sections {
		if len(section) > 0 {
			out[key] = section
		}
	}
	buf, err := yaml.Marshal(out)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal merged YAML: %w", err)
	}
	return string(buf), sources, nil
}

// getMergedConfigFiles merges all Flex config files in the given directory.
// It is a convenience wrapper for findFlexConfigFiles + mergeConfigFiles.
func getMergedConfigFiles(fsys fs.FS, dir, configDir string) (merged string, sources map[string]string, err error) {
	files, err := findFlexConfigFiles(fsys, dir, configDir)
	if err != nil {
		return "", nil, err
	}
	return mergeConfigFiles(fsys, files)
}
