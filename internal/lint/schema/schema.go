package schema

import (
	_ "embed"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

var (
	// upsun-config-schema.json is the self-contained Flex schema, originally from
	// platformify and now maintained here. Type/version validation is done by
	// CheckTypes against the registry, not by the schema.
	//go:embed upsun-config-schema.json
	configSchema    []byte
	parsedSchema    *gojsonschema.Schema
	parseSchemaOnce sync.Once
	parseSchemaErr  error
)

// Load loads the Upsun (Flex-style) configuration schema.
func Load() (*gojsonschema.Schema, error) {
	parseSchemaOnce.Do(func() {
		parsedSchema, parseSchemaErr = gojsonschema.NewSchema(gojsonschema.NewBytesLoader(configSchema))
	})
	return parsedSchema, parseSchemaErr
}
