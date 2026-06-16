package schema

import (
	_ "embed"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

var (
	// upsun-config-schema.json is the self-contained Flex schema from platformify,
	// refreshed by `make lint-assets`. The meta.upsun.com schema is not used here:
	// it resolves type/version via remote $refs (fetched at runtime) and duplicates
	// the registry-based type check. Type/version validation is done by CheckTypes.
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
