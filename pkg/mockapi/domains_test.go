package mockapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDefaultBranchDomains checks that the default branch's domains are the project's domains.
func TestDefaultBranchDomains(t *testing.T) {
	h := NewHandler(t)
	assert.Panics(t, func() { h.SetEnvironmentDomains("p", "main", nil) }, "the project must be set first")
	h.SetProjects([]*Project{{ID: "p", DefaultBranch: "main"}})
	h.SetEnvironmentDomains("p", "main", []*Domain{{ID: "example.com", Name: "example.com"}})
	h.SetEnvironmentDomains("p", "staging", []*Domain{{ID: "staging.example.com", Name: "staging.example.com"}})

	names := func(domains []*Domain) []string {
		result := make([]string, 0, len(domains))
		for _, d := range domains {
			result = append(result, d.Name)
		}
		return result
	}
	assert.Equal(t, []string{"example.com"}, names(h.Domains("p", "")))
	assert.Equal(t, []string{"example.com"}, names(h.Domains("p", "main")))
	assert.Equal(t, []string{"staging.example.com"}, names(h.Domains("p", "staging")))
}
