package lint

import (
	"fmt"
	"regexp"
	"strings"
)

const maxServiceNameLength = 32

var (
	isValidServiceNameChars = regexp.MustCompile(`^[a-z0-9_\-]+$`).MatchString
	startsEndsAlphanumeric  = regexp.MustCompile(`^[a-z0-9].*[a-z0-9]$`).MatchString
	isAlphanumeric          = regexp.MustCompile(`^[a-z0-9]+$`).MatchString
)

// CheckNames checks that application, service and worker names are correct.
func CheckNames(cfg *Config) *Result {
	result := &Result{}

	for name, app := range cfg.Applications {
		if err := validateServiceName(name, "application"); err != "" {
			result.AddError("applications."+name, err)
		}
		for workerName := range app.Workers {
			if err := validateServiceName(workerName, "worker"); err != "" {
				result.AddError("applications."+name+".workers."+workerName, err)
			}
		}
	}
	for name := range cfg.Services {
		if err := validateServiceName(name, "service"); err != "" {
			result.AddError("services."+name, err)
		}
	}

	return result
}

// validateServiceName checks that a service name is correct and returns an error message if not.
func validateServiceName(value, nameType string) string {
	if value == "router" {
		return fmt.Sprintf("%q is a reserved name.", value)
	}

	if len(value) > maxServiceNameLength {
		return fmt.Sprintf("%q is not a valid %s name, it should be shorter than %d characters.",
			value, nameType, maxServiceNameLength)
	}

	if strings.Contains(value, "--") {
		return fmt.Sprintf("%q is not a valid %s name, it should not contain double dashes.", value, nameType)
	}

	if !isValidServiceNameChars(value) {
		return fmt.Sprintf("%q is not a valid %s name, it can only contain lowercase alphanumeric characters, dashes, or underscores.", //nolint:lll
			value, nameType)
	}

	if len(value) == 1 {
		// Single character names are valid if they're alphanumeric
		if !isAlphanumeric(value) {
			return fmt.Sprintf("%q is not a valid %s name, it should start and end with alphanumeric characters.",
				value, nameType)
		}
	} else if !startsEndsAlphanumeric(value) {
		return fmt.Sprintf("%q is not a valid %s name, it should start and end with alphanumeric characters.",
			value, nameType)
	}

	return ""
}
