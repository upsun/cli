package schema

import (
	_ "embed"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

// Embedded Fixed-style (legacy Platform.sh) schemas, one per file type.
var (
	//go:embed platformsh.application.json
	applicationSchema []byte
	//go:embed platformsh.routes.json
	routesSchema []byte
	//go:embed platformsh.services.json
	servicesSchema []byte

	fixedOnce        sync.Once
	parsedApplicaton *gojsonschema.Schema
	parsedRoutes     *gojsonschema.Schema
	parsedServices   *gojsonschema.Schema
	fixedErr         error
)

func loadFixed() error {
	fixedOnce.Do(func() {
		if parsedApplicaton, fixedErr = gojsonschema.NewSchema(
			gojsonschema.NewBytesLoader(applicationSchema)); fixedErr != nil {
			return
		}
		if parsedRoutes, fixedErr = gojsonschema.NewSchema(
			gojsonschema.NewBytesLoader(routesSchema)); fixedErr != nil {
			return
		}
		parsedServices, fixedErr = gojsonschema.NewSchema(gojsonschema.NewBytesLoader(servicesSchema))
	})
	return fixedErr
}

// LoadApplication loads the Fixed-style application (.platform.app.yaml) schema.
func LoadApplication() (*gojsonschema.Schema, error) {
	if err := loadFixed(); err != nil {
		return nil, err
	}
	return parsedApplicaton, nil
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
