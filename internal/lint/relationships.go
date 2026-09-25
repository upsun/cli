package lint

import (
	"fmt"
	"strings"
)

// CheckRelationships checks that application, service and task names are unique,
// and that application and task relationships correspond to existing services or
// applications.
func CheckRelationships(cfg *Config) *Result {
	result := &Result{}

	// Applications, services and tasks share a namespace.
	sections := make(map[string]string)
	addName := func(name, section string) {
		if prev, exists := sections[name]; exists {
			result.AddError(section+"."+name,
				fmt.Sprintf("duplicate name found: '%s' in '%s' (previous in '%s')", name, section, prev))
			return
		}
		sections[name] = section
	}
	for name := range cfg.Applications {
		addName(name, keyApplications)
	}
	for name := range cfg.Services {
		addName(name, keyServices)
	}
	for name := range cfg.Tasks {
		addName(name, keyTasks)
	}

	// Worker names are scoped to their application, so they are tracked
	// separately: two applications may each define a worker with the same name,
	// and a worker may share a name with an application or service.
	workerNames := make(map[string]struct{})
	for appName := range cfg.Applications {
		for name := range cfg.Applications[appName].Workers {
			workerNames[name] = struct{}{}
		}
	}

	linkedServices := make(map[string]struct{})
	checkRelationships := func(kind, name, path string, relationships map[string]any) {
		for relName, value := range relationships {
			target, explicit := relationshipTarget(relName, value)
			relPath := path + ".relationships." + relName
			section, isNamed := sections[target]
			_, isWorker := workerNames[target]
			switch {
			case section == keyTasks:
				result.AddError(relPath, fmt.Sprintf(
					"relationship '%s' in %s '%s' points to task '%s', but a task cannot be a relationship target",
					relName, kind, name, target))
			case isNamed || isWorker:
				linkedServices[target] = struct{}{}
			default:
				msg := fmt.Sprintf("relationship '%s' in %s '%s' does not match any service (or app)", relName, kind, name)
				if explicit {
					msg = fmt.Sprintf("relationship '%s' in %s '%s' points to a service (or app) named '%s' which is not found",
						relName, kind, name, target)
				}
				if len(cfg.Services) == 0 {
					msg += " (did you forget to define services?)"
				}
				result.AddError(relPath, msg)
			}
		}
	}
	for appName := range cfg.Applications {
		checkRelationships("application", appName, "applications."+appName, cfg.Applications[appName].Relationships)
	}
	for taskName := range cfg.Tasks {
		checkRelationships("task", taskName, "tasks."+taskName, cfg.Tasks[taskName].Relationships)
	}

	// A service can also be used through a mount (e.g. network storage).
	linkMounts := func(mounts map[string]Mount) {
		for _, m := range mounts {
			if m.Source == "service" && m.Service != "" {
				linkedServices[m.Service] = struct{}{}
			}
		}
	}
	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		linkMounts(app.Mounts)
		for _, w := range app.Workers {
			linkMounts(w.Mounts)
		}
	}
	for taskName := range cfg.Tasks {
		linkMounts(cfg.Tasks[taskName].Mounts)
	}

	for name := range cfg.Services {
		if _, linked := linkedServices[name]; !linked {
			result.AddError("services."+name, fmt.Sprintf("no application or task has a relationship to service '%s'", name))
		}
	}

	return result
}

// relationshipTarget returns the service (or application) that a relationship
// points to, and whether it is named explicitly. By default, the relationship
// links to the service with the same name.
// TODO validate the endpoint
func relationshipTarget(relName string, value any) (target string, explicit bool) {
	switch details := value.(type) {
	case map[string]any:
		if s, ok := details["service"].(string); ok {
			return s, true
		}
	case string:
		return strings.SplitN(details, ":", 2)[0], true
	}
	return relName, false
}
