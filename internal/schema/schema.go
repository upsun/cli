package schema

import (
	_ "embed"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

//go:generate go run gen.go

var (
	// upsun-config-schema.json is the Flex schema served by meta.upsun.com, and
	// updated with `make update-schema`.
	//go:embed upsun-config-schema.json
	configSchema    []byte
	parsedSchema    *gojsonschema.Schema
	parseSchemaOnce sync.Once
	parseSchemaErr  error
)

// remoteRefs are the schemas the Flex schema refers to on meta.upsun.com. They
// are replaced with empty schemas, so that loading does not need the network,
// as types and versions are checked by CheckTypes against the registry.
var remoteRefs = []string{
	"https://meta.upsun.com/schema/runtime-versions",
	"https://meta.upsun.com/schema/service-versions",
}

// Load loads the Upsun (Flex-style) configuration schema.
func Load() (*gojsonschema.Schema, error) {
	parseSchemaOnce.Do(func() {
		parsedSchema, parseSchemaErr = compile(gojsonschema.NewBytesLoader(configSchema))
	})
	return parsedSchema, parseSchemaErr
}

// compile compiles a schema, with the remote references replaced.
func compile(loader gojsonschema.JSONLoader) (*gojsonschema.Schema, error) {
	sl := gojsonschema.NewSchemaLoader()
	for _, u := range remoteRefs {
		if err := sl.AddSchema(u, gojsonschema.NewStringLoader("{}")); err != nil {
			return nil, err
		}
	}
	return sl.Compile(loader)
}
