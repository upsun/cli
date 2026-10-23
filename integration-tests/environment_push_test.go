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
	// Print push options to the push log.
	p.setHook(t, "pre-receive", "env | grep ^GIT_PUSH_OPTION_ >&2\n")

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
	assert.Contains(t, stdErr, "GIT_PUSH_OPTION_0=environment.status=active")
	assert.Equal(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/feature"))
	actions := p.actions("feature")
	if assert.Len(t, actions, 1) {
		assert.Equal(t, "environment.activate", actions[0].Type)
	}
}

// pushFailureExitCode is EnvironmentPushCommand::PUSH_FAILURE_EXIT_CODE.
const pushFailureExitCode = 87

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
			assertExitCode(t, pushFailureExitCode, err)
			assert.Contains(t, stdErr, c.wantStdErr)
		})
	}
}

// TestEnvironmentPushOptions checks the options that choose what is pushed where.
func TestEnvironmentPushOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		args     []string
		noRemote bool                            // Build the repository without the project's Git remote.
		setup    func(t *testing.T, repo string) // Runs before the push, after main is pushed to the served repository.
		wantErr  bool                            // Rejections are checked by ref, as Git's output varies.
		// wantStdErr is checked in the push log, which includes the push options printed by a hook.
		wantStdErr []string
		check      func(t *testing.T, p *gitProject, repo string)
	}{
		{
			name:       "target",
			args:       []string{"--target", "other"},
			wantStdErr: []string{"Pushing HEAD to the branch other of project"},
			check: func(t *testing.T, p *gitProject, repo string) {
				assert.Equal(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/other"))
			},
		},
		{
			name:    "non-fast-forward",
			setup:   amendHead,
			wantErr: true,
			check: func(t *testing.T, p *gitProject, repo string) {
				assert.NotEqual(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/main"))
			},
		},
		{
			name:  "force",
			args:  []string{"--force"},
			setup: amendHead,
			check: func(t *testing.T, p *gitProject, repo string) {
				assert.Equal(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/main"))
			},
		},
		{
			name: "force with lease",
			args: []string{"--force-with-lease"},
			setup: func(t *testing.T, repo string) {
				runGit(t, repo, "update-ref", "refs/remotes/platform-test/main", "HEAD")
				amendHead(t, repo)
			},
			check: func(t *testing.T, p *gitProject, repo string) {
				assert.Equal(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/main"))
			},
		},
		{
			name:    "force with lease, without a remote-tracking branch",
			args:    []string{"--force-with-lease"},
			setup:   amendHead,
			wantErr: true,
			check: func(t *testing.T, p *gitProject, repo string) {
				assert.NotEqual(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/main"))
			},
		},
		{
			name: "activate with parent and type",
			args: []string{"--target", "feature", "--activate", "--parent", "dev", "--type", "staging"},
			wantStdErr: []string{
				"The environment will be activated.",
				"GIT_PUSH_OPTION_1=environment.parent=dev",
				"GIT_PUSH_OPTION_2=environment.type=staging",
			},
			check: func(t *testing.T, p *gitProject, repo string) {
				assert.Equal(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/feature"))
				if e := p.env("feature"); assert.NotNil(t, e) {
					assert.Equal(t, "dev", e.Parent)
					assert.Equal(t, "staging", e.Type)
				}
				actions := p.actions("feature")
				if assert.Len(t, actions, 1) {
					assert.Equal(t, "environment.activate", actions[0].Type)
				}
			},
		},
		{
			name:       "set upstream",
			args:       []string{"-p", layoutProjectID, "--target", "main", "--set-upstream"},
			noRemote:   true,
			wantStdErr: []string{"Mapping the directory"},
			check: func(t *testing.T, p *gitProject, repo string) {
				assert.Equal(t, gitRef(t, repo, "HEAD"), gitRef(t, p.repo, "refs/heads/main"))
				assertTrimmed(t, layoutGitURL, gitConfig(t, repo, "remote.platform-test.url"))
				assertTrimmed(t, "platform-test", gitConfig(t, repo, "branch.main.remote"))
				assertTrimmed(t, "refs/heads/main", gitConfig(t, repo, "branch.main.merge"))
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			feature := makeEnv(layoutProjectID, "feature", "development", "inactive", "main")
			feature.AddOperations("activate")
			p := setUpGitProject(t,
				makeEnv(layoutProjectID, "main", "production", "active", nil),
				makeEnv(layoutProjectID, "dev", "development", "active", "main"),
				feature,
			)
			// Print push options to the push log.
			p.setHook(t, "pre-receive", "env | grep ^GIT_PUSH_OPTION_ >&2\n")

			layout := repoLayout{git: true, branch: "main", remoteName: "platform-test"}
			if c.noRemote {
				layout.remoteName = ""
			}
			repo := layout.build(t)
			p.seed(t, repo, "main")
			if c.setup != nil {
				c.setup(t, repo)
			}
			p.f.dir = repo

			_, stdErr, err := p.f.RunCombinedOutput(append([]string{"push", "-y"}, c.args...)...)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			for _, s := range c.wantStdErr {
				assert.Contains(t, stdErr, s)
			}
			c.check(t, p, repo)
		})
	}
}

// amendHead replaces the HEAD commit, so that pushing it is not a fast-forward.
func amendHead(t *testing.T, repo string) {
	t.Helper()
	runGit(t, repo, "commit", "--quiet", "--amend", "--allow-empty", "-m", "Amended")
}
