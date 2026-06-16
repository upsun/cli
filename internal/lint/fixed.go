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

// Configuration section keys and the legacy per-app config file name.
const (
	keyApplications = "applications"
	keyServices     = "services"
	keyRoutes       = "routes"
	fixedAppConfig  = ".platform.app.yaml"
)

// flexTopKeys are the top-level keys that indicate Flex-style configuration.
var flexTopKeys = []string{keyApplications, keyServices, keyRoutes}

// lintFixed lints legacy Platform.sh (Fixed-style) configuration in dir:
// .platform.app.yaml files and/or .platform/applications.yaml, plus optional
// .platform/routes.yaml and .platform/services.yaml.
func lintFixed(_ context.Context, dir string) (*Result, error) {
	result := &Result{}

	apps, err := loadFixedApplications(dir, result)
	if err != nil {
		return nil, err
	}

	services, err := loadFixedSection(dir, "services.yaml", schema.LoadServices, result)
	if err != nil {
		return nil, err
	}
	routes, err := loadFixedSection(dir, "routes.yaml", schema.LoadRoutes, result)
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
	cfg, err := DecodeConfig(string(mergedYAML))
	if err != nil {
		return nil, err
	}

	checks, err := runChecks(cfg, StyleFixed)
	if err != nil {
		return nil, err
	}
	return Combine(result, checks), nil
}

// loadFixedApplications collects applications from .platform.app.yaml files and
// .platform/applications.yaml, validating each against the application schema.
func loadFixedApplications(dir string, result *Result) (map[string]any, error) {
	appSchema, err := schema.LoadApplication()
	if err != nil {
		return nil, fmt.Errorf("failed to load application schema: %w", err)
	}

	apps := map[string]any{}
	add := func(name, source string, data map[string]any) {
		if _, dup := apps[name]; dup {
			result.AddError(source, fmt.Sprintf("duplicate application name %q", name))
			return
		}
		apps[name] = data
	}

	// Individual .platform.app.yaml files.
	for _, abs := range findFixedAppFiles(dir) {
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
			result.AddError(source, "this looks like Flex (.upsun) configuration in a Fixed-style file")
			continue
		}
		*result = *Combine(result, CheckSchemaScoped(data, appSchema, source))
		add(fixedAppName(data), source, data)
	}

	// .platform/applications.yaml (a list of apps, or a map keyed by app name).
	appsFile := filepath.Join(dir, ".platform", "applications.yaml")
	raw, err := os.ReadFile(appsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return apps, nil
		}
		result.AddError(".platform/applications.yaml", err.Error())
		return apps, nil
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		result.AddError(".platform/applications.yaml", interpretYAMLError(err))
		return apps, nil
	}
	switch v := doc.(type) {
	case []any:
		for i, item := range v {
			data, ok := toStringMap(item)
			if !ok {
				result.AddError(fmt.Sprintf(".platform/applications.yaml[%d]", i), "application must be a map")
				continue
			}
			src := fmt.Sprintf(".platform/applications.yaml[%d]", i)
			*result = *Combine(result, CheckSchemaScoped(data, appSchema, src))
			add(fixedAppName(data), src, data)
		}
	case map[string]any:
		for name, item := range v {
			if strings.HasPrefix(name, ".") {
				continue
			}
			data, ok := toStringMap(item)
			if !ok {
				result.AddError(".platform/applications.yaml: "+name, "application must be a map")
				continue
			}
			// In map form the name comes from the key, not the value.
			if _, ok := data["name"]; !ok {
				data["name"] = name
			}
			src := ".platform/applications.yaml: " + name
			*result = *Combine(result, CheckSchemaScoped(data, appSchema, src))
			add(name, src, data)
		}
	case nil:
		// Empty file.
	default:
		result.AddError(".platform/applications.yaml", "contents must be a YAML list or map")
	}

	return apps, nil
}

// loadFixedSection reads and schema-validates an optional .platform/<file>,
// returning its decoded map (keyed by name/URL).
func loadFixedSection(
	dir, file string,
	loadSchema func() (*gojsonschema.Schema, error),
	result *Result,
) (map[string]any, error) {
	path := filepath.Join(dir, ".platform", file)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		result.AddError(".platform/"+file, err.Error())
		return nil, nil
	}
	data := map[string]any{}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		result.AddError(".platform/"+file, interpretYAMLError(err))
		return nil, nil
	}
	if len(data) == 0 {
		return nil, nil
	}
	sch, err := loadSchema()
	if err != nil {
		return nil, fmt.Errorf("failed to load %s schema: %w", file, err)
	}
	*result = *Combine(result, CheckSchemaScoped(data, sch, ".platform/"+file))
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

func toStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	default:
		return nil, false
	}
}

// relTo returns abs relative to dir, falling back to abs on error.
func relTo(dir, abs string) string {
	if rel, err := filepath.Rel(dir, abs); err == nil {
		return rel
	}
	return abs
}
