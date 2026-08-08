package lint

import (
	"fmt"
	"strings"
)

// CheckRelationships checks that application relationships correspond to existing services.
func CheckRelationships(cfg *Config) *Result {
	result := &Result{}

	var serviceNames = make(map[string]string)
	addService := func(name, source string) string {
		if prevSource, exists := serviceNames[name]; exists {
			return fmt.Sprintf("duplicate name found: '%s' in '%s' (previous in '%s')", name, source, prevSource)
		}
		serviceNames[name] = source
		return ""
	}
	// Worker names are scoped to their application, so they are tracked
	// separately: two applications may each define a worker with the same name,
	// and a worker may share a name with an application or service.
	var workerNames = make(map[string]struct{})
	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		if msg := addService(appName, "applications"); msg != "" {
			result.AddError("applications."+appName, msg)
		}
		for name := range app.Workers {
			workerNames[name] = struct{}{}
		}
	}
	for name := range cfg.Services {
		if msg := addService(name, "services"); msg != "" {
			result.AddError("services."+name, msg)
		}
	}

	var linkedServices = make(map[string]struct{})
	for appName := range cfg.Applications {
		appConfig := cfg.Applications[appName]
		for relName, value := range appConfig.Relationships {
			// By default, the relationship links to the service with the same name.
			var relationshipService = relName
			var explicit bool

			// The service name can also be specified explicitly, via a map or a string.
			// TODO validate the endpoint
			switch details := value.(type) {
			case map[string]any:
				if s, ok := details["service"].(string); ok {
					relationshipService = s
					explicit = true
				}
			case string:
				relationshipService = strings.SplitN(details, ":", 2)[0]
				explicit = true
			}

			_, isNamed := serviceNames[relationshipService]
			if _, isWorker := workerNames[relationshipService]; isNamed || isWorker {
				linkedServices[relationshipService] = struct{}{}
			} else {
				var msg string
				if explicit {
					msg = fmt.Sprintf(
						"relationship '%s' in application '%s' points to a service (or app) named '%s' which is not found",
						relName,
						appName,
						relationshipService,
					)
				} else {
					msg = fmt.Sprintf(
						"relationship '%s' in application '%s' does not match any service (or app)",
						relName,
						appName,
					)
				}
				if len(cfg.Services) == 0 {
					msg += " (did you forget to define services?)"
				}
				result.AddError("applications."+appName+".relationships."+relName, msg)
			}
		}
	}

	for name := range cfg.Services {
		if _, linked := linkedServices[name]; !linked {
			result.AddError("services."+name, fmt.Sprintf("no application has a relationship to service '%s'", name))
		}
	}

	return result
}
