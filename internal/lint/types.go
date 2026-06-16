package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/upsun/cli/internal/lint/registry"
)

// CheckTypes checks that application, service and worker types are supported images and versions.
func CheckTypes(cfg *Config, reg registry.Registry) *Result {
	result := &Result{}

	check := func(t string, runtime bool) error { return checkType(t, reg, runtime) }

	for appName, app := range cfg.Applications {
		if app.Type == "" && !isStackEmpty(app.Stack) {
			// For backwards compatibility, we allow 'stack' to be specified without 'type'.
			result.AddWarning("applications."+appName,
				"'type' should be specified (as a composable image) when using 'stack'")
			continue
		}
		if err := check(app.Type, true); err != nil {
			result.AddError("applications."+appName+".type", err.Error())
		}
		if strings.HasPrefix(app.Type, "composable") && isStackEmpty(app.Stack) {
			result.AddWarning("applications."+appName, "'stack' should be specified when using a composable image")
		}
	}
	for appName, app := range cfg.Applications {
		for workerName, w := range app.Workers {
			if w.Type != "" {
				if err := check(w.Type, true); err != nil {
					result.AddError("applications."+appName+".workers."+workerName+".type", err.Error())
				}
			}
		}
	}
	for serviceName, service := range cfg.Services {
		if err := check(service.Type, false); err != nil {
			result.AddError("services."+serviceName+".type", err.Error())
		}
	}

	return result
}

func checkType(t string, reg registry.Registry, runtime bool) error {
	if t == "" {
		return fmt.Errorf("type cannot be empty")
	}

	parts := strings.SplitN(t, ":", 2)
	var version string
	var imageType = parts[0]
	if len(parts) > 1 {
		version = parts[1]
	}

	if img, ok := reg[imageType]; ok {
		if img.IsRuntime && !runtime {
			return fmt.Errorf("type '%s' is a runtime type, not a service type", imageType)
		} else if !img.IsRuntime && runtime {
			return fmt.Errorf("type '%s' is a service type, not a runtime type", imageType)
		}

		// Allow supported or legacy versions, but only mention supported ones in the error.
		allVersions := append(img.Versions.Supported, img.Versions.Legacy...) //nolint:gocritic
		if !slices.Contains(allVersions, version) {
			if hasMajorVersion(allVersions, version) {
				return fmt.Errorf(
					"version '%s' is not precise enough for type '%s'; it must be exactly one of: %s",
					version, imageType, strings.Join(img.Versions.Supported, ", "))
			}
			return fmt.Errorf(
				"version '%s' is not supported for type '%s'; it must be exactly one of: %s",
				version, imageType, strings.Join(img.Versions.Supported, ", "))
		}
		return nil
	}

	return fmt.Errorf("type not found: '%s'; it must be one of: %s "+
		"(check the Registry for supported types, or make an application using a composable image)",
		imageType, strings.Join(reg.AllTypes(runtime), ", "))
}

func hasMajorVersion(l []string, v string) bool {
	for _, c := range l {
		if strings.HasPrefix(c, v+".") {
			return true
		}
	}
	return false
}
