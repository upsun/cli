package schema

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

// fixed-overlay.json is a JSON Merge Patch (RFC 7386), applied to the Flex
// schema to add the keys that only Fixed-style (Platform.sh) config accepts.
//
//go:embed fixed-overlay.json
var fixedOverlay []byte

var (
	fixedOnce         sync.Once
	parsedApplication *gojsonschema.Schema
	parsedRoutes      *gojsonschema.Schema
	parsedServices    *gojsonschema.Schema
	fixedErr          error
)

// fixedSchema returns the Flex schema with the Fixed overlay applied.
func fixedSchema() (map[string]any, error) {
	var root, patch map[string]any
	if err := json.Unmarshal(configSchema, &root); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(fixedOverlay, &patch); err != nil {
		return nil, err
	}
	mergePatch(root, patch)
	return root, nil
}

// mergePatch applies a JSON Merge Patch to target, modifying it.
func mergePatch(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	t, ok := target.(map[string]any)
	if !ok {
		t = map[string]any{}
	}
	for k, v := range p {
		if v == nil {
			delete(t, k)
		} else {
			t[k] = mergePatch(t[k], v)
		}
	}
	return t
}

// subSchema returns the schema at path in root, as a standalone schema.
func subSchema(root map[string]any, path ...string) (map[string]any, error) {
	node := root
	for _, k := range path {
		next, ok := node[k].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schema has no object at %v", path)
		}
		node = next
	}
	sub := make(map[string]any, len(node)+2)
	for k, v := range node {
		sub[k] = v
	}
	sub["$schema"] = root["$schema"]
	sub["definitions"] = root["definitions"]
	return sub, nil
}

func loadFixed() error {
	fixedOnce.Do(func() {
		root, err := fixedSchema()
		if err != nil {
			fixedErr = err
			return
		}
		for _, s := range []struct {
			parsed **gojsonschema.Schema
			path   []string
		}{
			{&parsedApplication, []string{"properties", "applications", "additionalProperties"}},
			{&parsedServices, []string{"properties", "services"}},
			{&parsedRoutes, []string{"properties", "routes"}},
		} {
			sub, err := subSchema(root, s.path...)
			if err != nil {
				fixedErr = err
				return
			}
			if *s.parsed, err = compile(gojsonschema.NewGoLoader(sub)); err != nil {
				fixedErr = err
				return
			}
		}
	})
	return fixedErr
}

// LoadApplication loads the Fixed-style application (.platform.app.yaml) schema.
func LoadApplication() (*gojsonschema.Schema, error) {
	if err := loadFixed(); err != nil {
		return nil, err
	}
	return parsedApplication, nil
}

// LoadRoutes loads the Fixed-style routes (.platform/routes.yaml) schema.
func LoadRoutes() (*gojsonschema.Schema, error) {
	if err := loadFixed(); err != nil {
		return nil, err
	}
	return parsedRoutes, nil
}

// LoadServices loads the Fixed-style services (.platform/services.yaml) schema.
func LoadServices() (*gojsonschema.Schema, error) {
	if err := loadFixed(); err != nil {
		return nil, err
	}
	return parsedServices, nil
}
