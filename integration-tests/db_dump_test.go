package tests

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestDbDumpNoEnvironment is a regression test for the fatal TypeError thrown
// by db:dump and db:sql when run outside a container without --environment.
// Both commands set envRequired=false on the Selector to support the
// in-container path (where local relationship env vars suffice), but the
// Selector then called selectRemoteContainer() with the resulting null
// environment.
func TestDbDumpNoEnvironment(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+url.PathEscape(projectID),
			"environments=/projects/"+url.PathEscape(projectID)+"/environments",
		),
		DefaultBranch: "main",
	}})

	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
		makeEnv(projectID, "staging", "staging", "active", "main"),
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	for _, cmd := range []string{"db:dump", "db:sql"} {
		t.Run(cmd, func(t *testing.T) {
			stdout, stderr, err := f.RunCombinedOutput(cmd, "-p", projectID)
			require.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)

			// The bug surfaced as a PHP TypeError on Selector::selectRemoteContainer.
			assert.NotContains(t, stderr, "TypeError")
			assert.NotContains(t, stderr, "selectRemoteContainer")
			assert.NotContains(t, stderr, "Fatal error")
			assert.Contains(t, stderr, "No environment specified")
		})
	}
}
