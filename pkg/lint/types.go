package lint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/upsun/cli/pkg/registry"
)

// checkTypes checks that application, worker, task and service types are supported images and versions.
func checkTypes(cfg *config, reg registry.Registry) *Result {
	result := &Result{}

	// check reports an error, or a warning for a retired or newer version, at path.
	check := func(path, t string, runtime bool) {
		warning, err := checkType(t, reg, runtime)
		if err != nil {
			result.addError(path, err.Error())
		} else if warning != "" {
			result.addWarning(path, warning)
		}
	}

	// checkRuntime checks the type, stack and image of an application or task at path.
	checkRuntime := func(path, t string, stack any, image *ociImage) {
		// A docker: type is built from an OCI image rather than a registry image.
		if strings.HasPrefix(t, "docker:") {
			switch {
			case image == nil || image.Name == "" && image.Buildfile == "":
				result.addError(path+".image", "either name or buildfile must be provided, both are missing")
			case image.Name != "" && image.Buildfile != "":
				result.addError(path+".image", "either name or buildfile must be provided, not both")
			}
			if !isStackEmpty(stack) {
				result.addWarning(path+".stack", "'stack' is only used with a composable image type")
			}
			return
		}
		if image != nil {
			result.addError(path+".image", "'image' is only used with a docker: type")
		}
		check(path+".type", t, true)
		isComposable := strings.HasPrefix(t, "composable")
		if isComposable && stack == nil {
			result.addWarning(path, "'stack' should be specified when using a composable image")
		} else if !isComposable && !isStackEmpty(stack) {
			result.addWarning(path+".stack", "'stack' is only used with a composable image type")
		}
	}

	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		checkRuntime("applications."+appName, app.Type, app.Stack, app.Image)
	}
	for taskName := range cfg.Tasks {
		// A task using a built-in definition ('base') has no type of its own.
		task := cfg.Tasks[taskName]
		if task.Base == "" {
			checkRuntime("tasks."+taskName, task.Type, task.Stack, task.Image)
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
// retired version or one newer than the known versions, or an error if the
// type or version is not allowed.
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
			// A version newer than all known ones may have been released since the registry was generated.
			if len(img.Versions.Supported) > 0 {
				if newest := slices.MaxFunc(allVersions, registry.CompareVersions); isNextVersion(version, newest) {
					return fmt.Sprintf("version '%s' of type '%s' is newer than the known versions, "+
						"so it could not be checked; the newest known version is %s", version, imageType, newest), nil
				}
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

// isNextVersion reports whether v could be a version released after newest. It
// must have as many parts, and leading zeros only where newest has them. At the
// first part that differs, it must be at most one higher for the major part, and
// at most 12 higher for others (e.g. month-numbered versions, from 26.05 to 26.11).
// This leaves typos such as "84", "8.44" or "8.06" for "8.4" as errors.
func isNextVersion(v, newest string) bool {
	vParts, newestParts := strings.Split(v, "."), strings.Split(newest, ".")
	if len(vParts) != len(newestParts) {
		return false
	}
	for i, p := range vParts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return false
		}
		if len(p) > 1 && p[0] == '0' && !strings.HasPrefix(newestParts[i], "0") {
			return false
		}
		if p == newestParts[i] {
			continue
		}
		vn, err := strconv.Atoi(p)
		if err != nil {
			return false
		}
		newestN, err := strconv.Atoi(newestParts[i])
		if err != nil {
			return false
		}
		maxStep := 12
		if i == 0 {
			maxStep = 1
		}
		return vn > newestN && vn-newestN <= maxStep
	}
	return false
}
