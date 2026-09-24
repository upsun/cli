package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestEnvironmentPushDetachedHead is a regression test for a TypeError raised
// by the legacy push command in a detached HEAD without --target.
func TestEnvironmentPushDetachedHead(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            projectID,
		DefaultBranch: "main",
		Repository:    mockapi.ProjectRepository{URL: "test-user@git.cli-tests.example.com:" + projectID + ".git"},
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	repo := t.TempDir()
	initRepoOnBranch(t, repo, "main")
	runGit(t, repo, "checkout", "--quiet", "--detach")

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.dir = repo

	_, stderr, err := f.RunCombinedOutput("push", "-p", projectID, "-y")
	require.Error(t, err)
	assert.NotContains(t, stderr, "TypeError")
	assert.Contains(t, stderr, "A target branch name (--target) is required.")
}
