package lint

import (
	"fmt"
	"sort"
	"strings"
)

// CheckRoutes validates that upstream routes link to existing applications or services.
func CheckRoutes(cfg *Config) *Result {
	result := &Result{}

	// Check if at least 1 route is defined when multiple applications exist
	if len(cfg.Applications) > 1 && len(cfg.Routes) == 0 {
		result.AddError("routes", "at least 1 route must be defined when multiple applications are defined")
	}

	// Build a set of valid upstream targets (app names and service names)
	validTargets := make(map[string]bool)

	// Add applications as valid targets
	for appName := range cfg.Applications {
		validTargets[appName] = true
	}

	// Add services as valid targets
	for serviceName := range cfg.Services {
		validTargets[serviceName] = true
	}

	// Check each upstream route
	for routeURL, route := range cfg.Routes {
		if route.Type != "upstream" {
			continue
		}
		if route.Upstream == "" {
			result.AddError(fmt.Sprintf("routes[%q].upstream", routeURL),
				"upstream property is required for routes with type 'upstream'")
			continue
		}

		// Parse the upstream value (format: "appname:protocol" or "servicename:protocol")
		upstream := route.Upstream
		parts := strings.SplitN(upstream, ":", 2)
		if len(parts) != 2 {
			result.AddError(fmt.Sprintf("routes[%q].upstream", routeURL),
				fmt.Sprintf("upstream '%s' must be in format 'name:protocol'", upstream))
			continue
		}

		targetName := parts[0]
		protocol := parts[1]

		// Check if the target exists
		if !validTargets[targetName] {
			// Build a helpful error message listing available targets
			availableTargets := make([]string, 0, len(validTargets))
			for target := range validTargets {
				availableTargets = append(availableTargets, target)
			}
			sort.Strings(availableTargets)

			if len(availableTargets) == 0 {
				result.AddError(fmt.Sprintf("routes[%q].upstream", routeURL),
					fmt.Sprintf("upstream target '%s' does not exist (no applications or services defined)", targetName))
			} else {
				result.AddError(fmt.Sprintf("routes[%q].upstream", routeURL),
					fmt.Sprintf("upstream target '%s' does not exist, available targets: %s",
						targetName, strings.Join(availableTargets, ", ")))
			}
		} else {
			// Target exists, validate the protocol is appropriate
			if _, isApp := cfg.Applications[targetName]; isApp {
				// For applications, protocol must be 'http'
				if protocol != "http" {
					result.AddError(fmt.Sprintf("routes[%q].upstream", routeURL),
						fmt.Sprintf("protocol '%s' is not valid for application '%s'; must be 'http'", protocol, targetName))
				}
			}
			// For services, protocol validation is complex and not implemented yet
			// No validation performed for service protocols
		}
	}

	return result
}
