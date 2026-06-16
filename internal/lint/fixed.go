package lint

import (
	"context"
	"fmt"
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
)

// flexTopKeys are the top-level keys that indicate Flex-style configuration.
var flexTopKeys = []string{keyApplications, keyServices, keyRoutes}

// lintFixed lints Fixed-style configuration in dir using the resolved names in
// cfg: per-app config files (cfg.app) and/or cfg.dir/applications.yaml, plus
// optional cfg.dir/routes.yaml and cfg.dir/services.yaml.
func lintFixed(_ context.Context, dir string, cfg fixedNames) (*Result, error) {
	result := &Result{}

	apps, err := loadFixedApplications(dir, cfg, result)
	if err != nil {
		return nil, err
	}

	services, err := loadFixedSection(dir, cfg.dir, keyServices, schema.LoadServices, result)
	if err != nil {
		return nil, err
	}
	routes, err := loadFixedSection(dir, cfg.dir, keyRoutes, schema.LoadRoutes, result)
	if err != nil {
		return nil, err
	}

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

	checks, err := runChecks(decoded, StyleFixed)
	if err != nil {
		return nil, err
	}
	result.Merge(checks)
	return result, nil
}

// loadFixedApplications collects applications from per-app config files and
// cfg.dir/applications.yaml, validating each against the application schema.
func loadFixedApplications(dir string, cfg fixedNames, result *Result) (map[string]any, error) {
	appSchema, err := schema.LoadApplication()
	if err != nil {
		return nil, fmt.Errorf("failed to load application schema: %w", err)
	}

	apps := map[string]any{}
	sources := map[string]string{}
	add := func(name, source string, data map[string]any) {
		if _, dup := apps[name]; dup {
			result.AddError(source, fmt.Sprintf(
				"duplicate application name %q (already defined in %s)", name, sources[name]))
			return
		}
		apps[name] = data
		sources[name] = source
	}

	// Individual per-app config files (e.g. .platform.app.yaml).
	for _, abs := range findFixedAppFiles(dir, cfg.app) {
		source := relTo(dir, abs)
		data, err := readYAMLMap(abs)
		if err != nil {
			result.AddError(source, err.Error())
			continue
		}
		if data == nil {
			continue
		}
		if hasAnyKey(data, flexTopKeys) {
			result.AddError(source, "this looks like Flex configuration in a Fixed-style file")
			continue
		}
		result.Merge(CheckSchemaScoped(data, appSchema, source))
		add(fixedAppName(data), source, data)
	}

	// cfg.dir/applications.yaml (a list of apps, or a map keyed by app name).
	appsFile, ok := firstExistingYAML(dir, cfg.dir, keyApplications)
	if !ok {
		return apps, nil
	}
	label := relTo(dir, appsFile)
	raw, err := os.ReadFile(appsFile)
	if err != nil {
		result.AddError(label, err.Error())
		return apps, nil
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		result.AddError(label, interpretYAMLError(err))
		return apps, nil
	}
	switch v := doc.(type) {
	case []any:
		for i, item := range v {
			data, ok := toStringMap(item)
			if !ok {
				result.AddError(fmt.Sprintf("%s[%d]", label, i), "application must be a map")
				continue
			}
			src := fmt.Sprintf("%s[%d]", label, i)
			result.Merge(CheckSchemaScoped(data, appSchema, src))
			add(fixedAppName(data), src, data)
		}
	case map[string]any:
		for name, item := range v {
			if strings.HasPrefix(name, ".") {
				continue
			}
			data, ok := toStringMap(item)
			if !ok {
				result.AddError(label+": "+name, "application must be a map")
				continue
			}
			src := label + ": " + name
			// In map form the name comes from the key and must not be set in the value.
			if _, ok := data["name"]; ok {
				result.AddError(src, "the application name must not be set here; it is taken from the key")
				continue
			}
			data["name"] = name
			result.Merge(CheckSchemaScoped(data, appSchema, src))
			add(name, src, data)
		}
	case nil:
		// Empty file.
	default:
		result.AddError(label, "contents must be a YAML list or map")
	}

	return apps, nil
}

// loadFixedSection reads and schema-validates an optional cfg.dir/<base>.{yaml,yml},
// returning its decoded map (keyed by name/URL).
func loadFixedSection(
	dir, configDir, base string,
	loadSchema func() (*gojsonschema.Schema, error),
	result *Result,
) (map[string]any, error) {
	path, ok := firstExistingYAML(dir, configDir, base)
	if !ok {
		return nil, nil
	}
	label := relTo(dir, path)
	raw, err := os.ReadFile(path)
	if err != nil {
		result.AddError(label, err.Error())
		return nil, nil
	}
	data := map[string]any{}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		result.AddError(label, interpretYAMLError(err))
		return nil, nil
	}
	if len(data) == 0 {
		return nil, nil
	}
	sch, err := loadSchema()
	if err != nil {
		return nil, fmt.Errorf("failed to load %s schema: %w", base, err)
	}
	result.Merge(CheckSchemaScoped(data, sch, label))
	return data, nil
}

// fixedAppName returns an application's name from its config, defaulting to "app".
func fixedAppName(data map[string]any) string {
	if name, ok := data["name"].(string); ok && name != "" {
		return name
	}
	return "app"
}

func readYAMLMap(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("%s", interpretYAMLError(err))
	}
	return data, nil
}

func hasAnyKey(m map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// toStringMap asserts that v is a YAML map. yaml.v3 always decodes mappings to
// map[string]any, so a plain type assertion suffices.
func toStringMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// relTo returns abs relative to dir, falling back to abs on error.
func relTo(dir, abs string) string {
	if rel, err := filepath.Rel(dir, abs); err == nil {
		return rel
	}
	return abs
}
