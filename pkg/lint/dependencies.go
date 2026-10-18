package lint

import "fmt"

// checkDependencies validates the application dependencies section.
func checkDependencies(cfg *config) *Result {
	result := &Result{}

	validTypes := map[string]bool{
		"nodejs":  true,
		"php":     true,
		"python":  true,
		"python3": true,
		"ruby":    true,
	}

	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		for depType, packages := range app.Dependencies {
			// Lint dependency type
			if !validTypes[depType] {
				path := "applications." + appName + ".dependencies." + depType
				msg := fmt.Sprintf("invalid dependency type '%s'; must be one of: nodejs, php, python, python3, ruby", depType)
				result.addError(path, msg)
				continue
			}

			// Lint package names and versions are not empty
			for pkgName, version := range packages {
				if pkgName == "" {
					path := "applications." + appName + ".dependencies." + depType
					result.addError(path, "package name cannot be empty")
				}
				if version == "" {
					path := "applications." + appName + ".dependencies." + depType + "." + pkgName
					result.addError(path, "package version cannot be empty")
				}
			}
		}
	}

	return result
}
