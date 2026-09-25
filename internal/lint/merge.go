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
// an error. It returns the merged YAML and where each entry is defined.
func mergeConfigFiles(fsys fs.FS, files []string) (merged string, sources sourceIndex, err error) {
	sections := map[string]map[string]any{}
	sources = sourceIndex{}
	for _, file := range files {
		doc, err := loadYAML(fsys, file)
		if err != nil {
			return "", nil, err
		}
		if doc == nil {
			continue
		}
		if doc.Kind != yaml.MappingNode {
			return "", nil, &sourceError{file: file, line: doc.Line, msg: "contents should be a YAML map"}
		}
		for i := 0; i+1 < len(doc.Content); i += 2 {
			keyNode, section := doc.Content[i], doc.Content[i+1]
			key := keyNode.Value
			if strings.HasPrefix(key, ".") {
				continue
			}
			if !slices.Contains(flexTopKeys, key) {
				return "", nil, &sourceError{file: file, line: keyNode.Line, msg: fmt.Sprintf(
					"unknown top-level key '%s': it must be one of: %s", key, strings.Join(flexTopKeys, ", "))}
			}
			if section.Tag == "!!null" {
				continue
			}
			if section.Kind != yaml.MappingNode {
				return "", nil, &sourceError{file: file, line: keyNode.Line, msg: key + " is not a map"}
			}
			if sections[key] == nil {
				sections[key] = map[string]any{}
			}
			for j := 0; j+1 < len(section.Content); j += 2 {
				nameNode, valueNode := section.Content[j], section.Content[j+1]
				name := nameNode.Value
				if prev, exists := sources[key+"."+name]; exists {
					return "", nil, &sourceError{file: file, line: nameNode.Line, msg: fmt.Sprintf(
						"duplicate key '%s' in section '%s' (already defined in %s)", name, key, prev.file)}
				}
				var v any
				if err := valueNode.Decode(&v); err != nil {
					return "", nil, &sourceError{file: file, line: nameNode.Line, msg: err.Error()}
				}
				sections[key][name] = v
				sources[key+"."+name] = source{file: file, line: nameNode.Line, node: valueNode}
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
func getMergedConfigFiles(fsys fs.FS, dir, configDir string) (merged string, sources sourceIndex, err error) {
	files, err := findFlexConfigFiles(fsys, dir, configDir)
	if err != nil {
		return "", nil, err
	}
	return mergeConfigFiles(fsys, files)
}
