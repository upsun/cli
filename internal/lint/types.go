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

	// check reports an error, or a warning for a retired version, at path.
	check := func(path, t string, runtime bool) {
		warning, err := checkType(t, reg, runtime)
		if err != nil {
			result.AddError(path, err.Error())
		} else if warning != "" {
			result.AddWarning(path, warning)
		}
	}

	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		if app.Type == "" && !isStackEmpty(app.Stack) {
			// For backwards compatibility, we allow 'stack' to be specified without 'type'.
			result.AddWarning("applications."+appName,
				"'type' should be specified (as a composable image) when using 'stack'")
			continue
		}
		check("applications."+appName+".type", app.Type, true)
		isComposable := strings.HasPrefix(app.Type, "composable")
		if isComposable && isStackEmpty(app.Stack) {
			result.AddWarning("applications."+appName, "'stack' should be specified when using a composable image")
		} else if !isComposable && !isStackEmpty(app.Stack) {
			result.AddWarning("applications."+appName+".stack", "'stack' is only used with a composable image type")
		}
	}
	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		for workerName, w := range app.Workers {
			if w.Type != "" {
				check("applications."+appName+".workers."+workerName+".type", w.Type, true)
			}
		}
	}
	for serviceName, service := range cfg.Services {
		check("services."+serviceName+".type", service.Type, false)
	}

	return result
}

// checkType validates an image type and version. It returns a warning for a
// retired version, or an error if the type or version is not allowed.
func checkType(t string, reg registry.Registry, runtime bool) (string, error) {
	if t == "" {
		return "", fmt.Errorf("type cannot be empty")
	}

	parts := strings.SplitN(t, ":", 2)
	var version string
	var imageType = parts[0]
	if len(parts) > 1 {
		version = parts[1]
	}

	if img, ok := reg[imageType]; ok {
		if img.IsRuntime && !runtime {
			return "", fmt.Errorf("type '%s' is a runtime type, not a service type", imageType)
		} else if !img.IsRuntime && runtime {
			return "", fmt.Errorf("type '%s' is a service type, not a runtime type", imageType)
		}

		// Suggest supported versions, if there are any.
		var useOneOf, mustBeOneOf string
		if len(img.Versions.Supported) > 0 {
			supported := strings.Join(img.Versions.Supported, ", ")
			useOneOf = "; use one of: " + supported
			mustBeOneOf = "; it must be exactly one of: " + supported
		}
		if slices.Contains(img.Versions.Retired, version) {
			return fmt.Sprintf("version '%s' of type '%s' is retired%s", version, imageType, useOneOf), nil
		}

		// Allow supported or legacy versions, but only mention supported ones in the error.
		allVersions := slices.Concat(img.Versions.Supported, img.Versions.Legacy, img.Versions.Retired)
		if !slices.Contains(allVersions, version) {
			if hasMajorVersion(allVersions, version) {
				return "", fmt.Errorf("version '%s' is not precise enough for type '%s'%s",
					version, imageType, mustBeOneOf)
			}
			return "", fmt.Errorf("version '%s' is not supported for type '%s'%s",
				version, imageType, mustBeOneOf)
		}
		return "", nil
	}

	return "", fmt.Errorf("type not found: '%s'; it must be one of: %s "+
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
