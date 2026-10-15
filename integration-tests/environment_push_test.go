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
	t.Parallel()
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

func TestEnvironmentPush(t *testing.T) {
	t.Parallel()
	main := makeEnv(layoutProjectID, "main", "production", "active", nil)
	feature := makeEnv(layoutProjectID, "feature", "development", "inactive", "main")
	feature.AddOperations("activate")
	p := setUpGitProject(t, main, feature)
	// Record push options, and the pushed refs.
	p.setHook(t, "pre-receive", `for i in $(seq 0 $((GIT_PUSH_OPTION_COUNT - 1))); do
  eval "echo \"push option: \$GIT_PUSH_OPTION_$i\"" >&2
done
`)

	repo := repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)
	p.f.dir = repo

	_, stdErr, err := p.f.RunCombinedOutput("push", "-y")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Pushing HEAD to the environment main")
	assert.Equal(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/main"))

	// Push a new commit to an inactive environment, and activate it.
	runGit(t, repo, "checkout", "--quiet", "-b", "feature")
	runGit(t, repo, "commit", "--quiet", "--allow-empty", "-m", "Feature")
	_, stdErr, err = p.f.RunCombinedOutput("push", "-y", "--activate")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "The environment will be activated.")
	assert.Contains(t, stdErr, "push option: environment.status=active")
	assert.Equal(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/feature"))
	actions := p.api.EnvironmentActions(layoutProjectID, "feature")
	if assert.Len(t, actions, 1) {
		assert.Equal(t, "environment.activate", actions[0].Type)
	}
}

// TestEnvironmentPushDeployError checks that deployment errors in the push log are detected.
func TestEnvironmentPushDeployError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		log        string
		wantStdErr string
	}{
		{
			name:       "build error",
			log:        "Unable to build application",
			wantStdErr: `The push completed but there was a deployment error ("Unable to build application").`,
		},
		{
			name:       "resources",
			log:        "Invalid deployment: Resources must be configured for new services",
			wantStdErr: "Configure resources for the environment by running: platform-test resources:set",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := setUpGitProject(t, makeEnv(layoutProjectID, "main", "production", "active", nil))
			p.setHook(t, "post-receive", "echo '"+c.log+"' >&2\n")
			p.f.dir = repoLayout{git: true, branch: "main", remoteName: "platform-test"}.build(t)

			_, stdErr, err := p.f.RunCombinedOutput("push", "-y")
			assertExitCode(t, 87, err)
			assert.Contains(t, stdErr, c.wantStdErr)
		})
	}
}
