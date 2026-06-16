package lint

import "fmt"

// CheckDependencies validates the application dependencies section.
func CheckDependencies(cfg *Config) *Result {
	result := &Result{}

	validTypes := map[string]bool{
		"nodejs":  true,
		"php":     true,
		"python":  true,
		"python3": true,
		"ruby":    true,
	}

	for appName, app := range cfg.Applications {
		for depType, packages := range app.Dependencies {
			// Lint dependency type
			if !validTypes[depType] {
				path := "applications." + appName + ".dependencies." + depType
				msg := fmt.Sprintf("invalid dependency type '%s'; must be one of: nodejs, php, python3, ruby", depType)
				result.AddError(path, msg)
				continue
			}

			// Lint package names and versions are not empty
			for pkgName, version := range packages {
				if pkgName == "" {
					path := "applications." + appName + ".dependencies." + depType
					result.AddError(path, "package name cannot be empty")
				}
				if version == "" {
					path := "applications." + appName + ".dependencies." + depType + "." + pkgName
					result.AddError(path, "package version cannot be empty")
				}
			}
		}
	}

	return result
}
