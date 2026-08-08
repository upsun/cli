package lint

import (
	"fmt"
	"strings"

	"github.com/dlclark/regexp2/v2"
)

// invalidPathChars contains characters that are invalid in URL paths without percent-encoding.
const invalidPathChars = "^#?<>[]{}\\|\"` "

// validateLocationPath checks if a location key is a valid URL path.
// It returns an error message if invalid, or an empty string if valid.
func validateLocationPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		return "must start with a slash"
	}
	if strings.ContainsAny(path, invalidPathChars) {
		return "contains invalid characters"
	}
	// Special case for "/" - valid as-is.
	if path == "/" {
		return ""
	}
	// Check for empty segments, "." or ".." segments.
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	for _, part := range parts[1:] { // Skip first empty part from leading slash.
		if part == "" || part == "." || part == ".." {
			return "cannot include empty parts, '.' or '..'"
		}
	}
	return ""
}

func CheckWebConfig(cfg *Config) *Result {
	result := &Result{}

	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		for locName, loc := range app.Web.Locations {
			// Validate location key format.
			locKeyPath := "applications." + appName + ".web.locations"
			if errMsg := validateLocationPath(locName); errMsg != "" {
				result.AddError(locKeyPath, fmt.Sprintf("location key %q %s", locName, errMsg))
				continue
			}

			path := "applications." + appName + ".web.locations[\"" + locName + "\"].root"

			// Lint root path
			if loc.Root != "" {
				// Check for leading slash (not allowed)
				if loc.Root == "/" {
					result.AddError(path, "the root cannot begin with a slash (use an empty string instead)")
					continue
				} else if strings.HasPrefix(loc.Root, "/") {
					result.AddError(path, "the root cannot begin with a slash")
					continue
				}

				// Check for invalid path components (no leading slash here)
				parts := strings.SplitSeq(strings.TrimSuffix(loc.Root, "/"), "/")
				for part := range parts {
					if part == "" || part == "." || part == ".." {
						path := "applications." + appName + ".web.locations[\"" + locName + "\"].root"
						result.AddError(path, "the root path cannot include empty parts, '.' or '..'")
						break
					}
				}
			}

			// Lint rules regular expressions
			for rulePattern := range loc.Rules {
				if rulePattern == "" {
					path := "applications." + appName + ".web.locations[\"" + locName + "\"].rules"
					result.AddError(path, "rule pattern cannot be empty")
					continue
				}

				// Use regexp2 to validate PCRE regex syntax (Nginx uses PCRE)
				_, err := regexp2.Compile(rulePattern)
				if err != nil {
					path := "applications." + appName + ".web.locations[\"" + locName + "\"].rules"
					result.AddError(path, "invalid regular expression: "+err.Error())
				}
			}
		}
	}
	return result
}
