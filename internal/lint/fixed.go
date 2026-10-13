package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"

	"github.com/upsun/cli/internal/lint/schema"
)

// Configuration section keys.
const (
	keyApplications = "applications"
	keyServices     = "services"
	keyRoutes       = "routes"
	keyTasks        = "tasks"
)

// flexTopKeys are the top-level keys of Flex-style configuration.
var flexTopKeys = []string{keyApplications, keyServices, keyRoutes, keyTasks}

// lintFixed lints Fixed-style configuration in dir using the resolved names in
// cfg: per-app config files (cfg.app) and/or cfg.dir/applications.yaml, plus
// optional cfg.dir/routes.yaml and cfg.dir/services.yaml.
func lintFixed(dir string, cfg fixedNames) (*Result, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	l := &fixedLoader{dir: dir, fsys: root.FS(), result: &Result{}, sources: sourceIndex{}}

	apps, err := l.loadApplications(cfg)
	if err != nil {
		return nil, err
	}
	services, err := l.loadSection(cfg.dir, keyServices, schema.LoadServices)
	if err != nil {
		return nil, err
	}
	routes, err := l.loadSection(cfg.dir, keyRoutes, schema.LoadRoutes)
	if err != nil {
		return nil, err
	}
	result := l.result

	if len(apps) == 0 && !result.HasErrors() {
		result.AddError("", "no application configuration found")
	}

	// If the structure is invalid, don't run semantic checks over a broken config.
	if result.HasErrors() {
		return result, nil
	}

	merged := map[string]any{keyApplications: apps}
	if len(services) > 0 {
		merged[keyServices] = services
	}
	if len(routes) > 0 {
		merged[keyRoutes] = routes
	}

	mergedYAML, err := yaml.Marshal(merged)
	if err != nil {
		return nil, err
	}
	decoded, err := DecodeConfig(string(mergedYAML))
	if err != nil {
		return nil, err
	}

	checks, err := runChecks(decoded)
	if err != nil {
		return nil, err
	}
	l.sources.locate(checks)
	result.Merge(checks)
	return result, nil
}

// fixedLoader reads Fixed-style files, collecting issues and where each
// application, service and route is defined.
type fixedLoader struct {
	dir     string
	fsys    fs.FS
	result  *Result
	sources sourceIndex
}

// load reads a YAML file (an absolute path under the project), reporting a
// problem as an issue and returning a nil node.
func (l *fixedLoader) load(abs string) (file string, node *yaml.Node) {
	file = filepath.ToSlash(relTo(l.dir, abs))
	node, err := loadYAML(l.fsys, file)
	var srcErr *sourceError
	if errors.As(err, &srcErr) {
		l.result.Errors = append(l.result.Errors, srcErr.issue())
		return file, nil
	} else if err != nil {
		l.result.Errors = append(l.result.Errors, Issue{File: file, Message: err.Error()})
		return file, nil
	}
	return file, node
}

// check validates data against a schema, locating issues within node.
func (l *fixedLoader) check(data any, sch *gojsonschema.Schema, key string, src source) {
	checked := CheckSchemaAt(data, sch, key)
	sourceIndex{key: src}.locate(checked)
	l.result.Merge(checked)
}

// loadApplications collects applications from per-app config files and
// cfg.dir/applications.yaml, validating each against the application schema.
func (l *fixedLoader) loadApplications(cfg fixedNames) (map[string]any, error) {
	appSchema, err := schema.LoadApplication()
	if err != nil {
		return nil, fmt.Errorf("failed to load application schema: %w", err)
	}

	apps := map[string]any{}
	add := func(name string, data map[string]any, src source) {
		key := keyApplications + "." + name
		if prev, dup := l.sources[key]; dup {
			l.result.Errors = append(l.result.Errors, Issue{
				File:    src.file,
				Line:    max(src.line, src.node.Line),
				Message: fmt.Sprintf("duplicate application name %q (already defined in %s)", name, prev.file),
			})
			return
		}
		l.check(data, appSchema, key, src)
		apps[name] = data
		l.sources[key] = src
	}
	decodeApp := func(file string, node *yaml.Node) (map[string]any, bool) {
		var data map[string]any
		if resolveAlias(node).Kind != yaml.MappingNode || node.Decode(&data) != nil {
			l.result.Errors = append(l.result.Errors, Issue{File: file, Line: node.Line,
				Message: "application must be a map"})
			return nil, false
		}
		return data, true
	}

	// Individual per-app config files (e.g. .platform.app.yaml).
	for _, abs := range findFixedAppFiles(l.dir, cfg.app) {
		file, node := l.load(abs)
		if node == nil {
			continue
		}
		data, ok := decodeApp(file, node)
		if !ok {
			continue
		}
		if hasAnyKey(data, flexTopKeys) {
			l.result.Errors = append(l.result.Errors, Issue{File: file, Line: node.Line,
				Message: "this looks like Flex configuration in a Fixed-style file"})
			continue
		}
		add(fixedAppName(data), data, source{file: file, node: node})
	}

	// cfg.dir/applications.yaml (a list of apps, or a map keyed by app name).
	appsFile, ok := firstExistingYAML(l.dir, cfg.dir, keyApplications)
	if !ok {
		return apps, nil
	}
	file, node := l.load(appsFile)
	if node == nil {
		return apps, nil
	}
	switch node = resolveAlias(node); node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if data, ok := decodeApp(file, item); ok {
				add(fixedAppName(data), data, source{file: file, node: item})
			}
		}
	case yaml.MappingNode:
		for _, entry := range mappingEntries(node) {
			name, item := entry.key.Value, entry.value
			if strings.HasPrefix(name, ".") {
				continue
			}
			data, ok := decodeApp(file, item)
			if !ok {
				continue
			}
			// In map form the name comes from the key and must not be set in the value.
			if _, ok := data["name"]; ok {
				l.result.Errors = append(l.result.Errors, Issue{File: file, Line: lineOf(item, ".name"),
					Message: "the application name must not be set here; it is taken from the key"})
				continue
			}
			data["name"] = name
			add(name, data, source{file: file, line: entry.key.Line, node: item})
		}
	default:
		l.result.Errors = append(l.result.Errors, Issue{File: file, Line: node.Line,
			Message: "contents must be a YAML list or map"})
	}

	return apps, nil
}

// loadSection reads and schema-validates an optional <configDir>/<section>.{yaml,yml},
// returning its decoded map (keyed by name/URL).
func (l *fixedLoader) loadSection(
	configDir, section string,
	loadSchema func() (*gojsonschema.Schema, error),
) (map[string]any, error) {
	abs, ok := firstExistingYAML(l.dir, configDir, section)
	if !ok {
		return nil, nil
	}
	file, node := l.load(abs)
	if node == nil {
		return nil, nil
	}
	data := map[string]any{}
	if resolveAlias(node).Kind != yaml.MappingNode || node.Decode(&data) != nil {
		l.result.Errors = append(l.result.Errors, Issue{File: file, Line: node.Line, Message: "contents must be a YAML map"})
		return nil, nil
	}
	if len(data) == 0 {
		return nil, nil
	}
	sch, err := loadSchema()
	if err != nil {
		return nil, fmt.Errorf("failed to load %s schema: %w", section, err)
	}
	l.sources[section] = source{file: file, node: node}
	for _, entry := range mappingEntries(node) {
		l.sources[section+"."+entry.key.Value] = source{file: file, line: entry.key.Line, node: entry.value}
	}
	checked := CheckSchemaAt(data, sch, section)
	l.sources.locate(checked)
	l.result.Merge(checked)
	return data, nil
}

// fixedAppName returns an application's name from its config, defaulting to "app".
func fixedAppName(data map[string]any) string {
	if name, ok := data["name"].(string); ok && name != "" {
		return name
	}
	return "app"
}

func hasAnyKey(m map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// relTo returns abs relative to dir, falling back to abs on error.
func relTo(dir, abs string) string {
	if rel, err := filepath.Rel(dir, abs); err == nil {
		return rel
	}
	return abs
}
