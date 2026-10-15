package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestEnvironmentCheckoutNumericID checks that "checkout" without an argument
// can offer environments whose IDs are all digits (which PHP turns into
// integer array keys).
func TestEnvironmentCheckoutNumericID(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	cases := []struct {
		name  string
		envs  []string // non-default environments, besides "main"
		stdin string
	}{
		{name: "one other environment", envs: []string{"123"}, stdin: "y\n"},
		{name: "several other environments", envs: []string{"123", "456"}, stdin: "0\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			apiHandler := mockapi.NewHandler(t)
			apiHandler.SetProjects([]*mockapi.Project{{
				ID:            layoutProjectID,
				Title:         "Checkout Test",
				DefaultBranch: "main",
				Repository:    mockapi.ProjectRepository{URL: layoutGitURL},
				Links: mockapi.MakeHALLinks(
					"self=/projects/"+layoutProjectID,
					"environments=/projects/"+layoutProjectID+"/environments",
				),
			}})
			envs := []*mockapi.Environment{makeEnv(layoutProjectID, "main", "production", "active", nil)}
			for _, e := range c.envs {
				envs = append(envs, makeEnv(layoutProjectID, e, "development", "active", "main"))
			}
			apiHandler.SetEnvironments(envs)
			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			f.dir = repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)
			runGit(t, f.dir, "branch", "123")

			_, stdErr, err := f.RunInteractive(c.stdin, "checkout")
			require.NoError(t, err, "stderr: %s", stdErr)
			assert.Contains(t, stdErr, "Checking out 123")
		})
	}
}

// TestEnvironmentCheckoutRemote checks out an environment which only exists on the remote.
func TestEnvironmentCheckoutRemote(t *testing.T) {
	t.Parallel()
	p := setUpGitProject(t,
		makeEnv(layoutProjectID, "main", "production", "active", nil),
		makeEnv(layoutProjectID, "feature", "development", "active", "main"),
	)
	repo := repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)
	runGit(t, repo, "checkout", "--quiet", "-b", "feature")
	runGit(t, repo, "commit", "--quiet", "--allow-empty", "-m", "Feature")
	p.seed(t, repo, "main", "feature")
	featureCommit := gitRef(t, repo, "feature")
	runGit(t, repo, "checkout", "--quiet", "main")
	runGit(t, repo, "branch", "--quiet", "-D", "feature")
	p.f.dir = repo

	_, stdErr, err := p.f.RunCombinedOutput("checkout", "feature")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Creating local branch feature based on upstream platform-test/feature")
	assert.Equal(t, featureCommit, gitRef(t, repo, "HEAD"))
	assert.Equal(t, "refs/heads/feature\n", gitConfig(t, repo, "branch.feature.merge"))
	assert.Equal(t, "platform-test\n", gitConfig(t, repo, "branch.feature.remote"))

	_, stdErr, err = p.f.RunCombinedOutput("checkout", "missing")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Branch not found: missing")
}
