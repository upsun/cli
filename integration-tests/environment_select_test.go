package tests

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// layoutProjectID is a 12+ character lowercase ID so it matches the Git URL
// detection pattern (which requires [a-z0-9]{12,}).
const layoutProjectID = "abcdefghijkl"

// layoutGitURL is a project Git URL the CLI can parse to detect the project.
// The test config's detection.git_domain is "git.cli-tests.example.com" and the
// parser requires a leading "git." host label, hence the doubled "git.".
const layoutGitURL = layoutProjectID + "@git.git.cli-tests.example.com:" + layoutProjectID + ".git"

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}

func initRepoOnBranch(t *testing.T, dir, branch string) {
	t.Helper()
	runGit(t, dir, "init", "--quiet", "--initial-branch="+branch)
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0o644))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "--quiet", "-m", "Initial commit")
}

func writeLayoutProjectConfig(t *testing.T, dir string) {
	t.Helper()
	configDir := filepath.Join(dir, ".platform", "local")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "project.yaml"), []byte("id: "+layoutProjectID+"\n"), 0o644))
}

// repoLayout describes a local checkout layout to exercise project and
// environment auto-detection.
type repoLayout struct {
	git         bool   // initialize a Git repository
	branch      string // branch to check out (Git repositories only)
	remoteName  string // add a remote with this name pointing at layoutGitURL ("" for none)
	projectYAML bool   // write .platform/local/project.yaml with the project ID
	worktree    bool   // check out branch in a worktree nested inside a parent on the default branch
}

// build creates the layout in a temporary directory and returns the directory
// the CLI should run in.
func (l repoLayout) build(t *testing.T) string {
	base := t.TempDir()

	if !l.git {
		if l.projectYAML {
			writeLayoutProjectConfig(t, base)
		}
		return base
	}

	if l.worktree {
		// The parent stays on the default branch; the worktree checks out l.branch.
		initRepoOnBranch(t, base, "main")
		if l.remoteName != "" {
			runGit(t, base, "remote", "add", l.remoteName, layoutGitURL)
		}
		worktree := filepath.Join(base, "nested", "worktree")
		runGit(t, base, "worktree", "add", "-b", l.branch, worktree)
		if l.projectYAML {
			writeLayoutProjectConfig(t, worktree)
		}
		return worktree
	}

	initRepoOnBranch(t, base, l.branch)
	if l.remoteName != "" {
		runGit(t, base, "remote", "add", l.remoteName, layoutGitURL)
	}
	if l.projectYAML {
		writeLayoutProjectConfig(t, base)
	}
	return base
}

// TestEnvironmentSelectionByLayout characterizes which environment the CLI
// auto-selects for a range of local checkout layouts. The project has two
// environments, "main" (the default) and "staging"; the CLI is expected to pick
// the environment matching the checked-out branch, identifying the project from
// either the Git remote or .platform/local/project.yaml.
func TestEnvironmentSelectionByLayout(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            layoutProjectID,
		Title:         "Layout Test",
		DefaultBranch: "main",
		Repository:    mockapi.ProjectRepository{URL: layoutGitURL},
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+layoutProjectID,
			"environments=/projects/"+layoutProjectID+"/environments",
		),
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(layoutProjectID, "main", "production", "active", nil),
		makeEnv(layoutProjectID, "staging", "development", "active", "main"),
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	cases := []struct {
		name    string
		layout  repoLayout
		wantEnv string // the environment "env:info" is expected to select
		wantErr string // a substring expected on stderr when no environment is selected
	}{
		{
			name:    "git repo on the default branch, project from Git remote",
			layout:  repoLayout{git: true, branch: "main", remoteName: "platform-test"},
			wantEnv: "main",
		},
		{
			name:    "git repo on a branch matching an environment",
			layout:  repoLayout{git: true, branch: "staging", remoteName: "platform-test"},
			wantEnv: "staging",
		},
		{
			name:    "git repo identified by project.yaml without a remote",
			layout:  repoLayout{git: true, branch: "staging", projectYAML: true},
			wantEnv: "staging",
		},
		{
			name:    "git worktree on a branch, nested in a parent on the default branch",
			layout:  repoLayout{git: true, worktree: true, branch: "staging", remoteName: "platform-test"},
			wantEnv: "staging",
		},
		{
			name:    "git repo on a branch with no matching environment",
			layout:  repoLayout{git: true, branch: "feature-x", remoteName: "platform-test"},
			wantErr: "Could not determine the current environment",
		},
		{
			name:    "git repo with a remote that is neither the configured name nor origin",
			layout:  repoLayout{git: true, branch: "main", remoteName: "github"},
			wantErr: "Could not determine the current project",
		},
		{
			name:    "no git repository, project.yaml present but not anchored to a repo",
			layout:  repoLayout{git: false, projectYAML: true},
			wantErr: "Could not determine the current project",
		},
		{
			name:    "no git repository and no project config",
			layout:  repoLayout{git: false},
			wantErr: "Could not determine the current project",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			f.dir = c.layout.build(t)

			stdOut, stdErr, err := f.RunCombinedOutput("env:info", "name")

			if c.wantErr != "" {
				require.Error(t, err, "expected a failure; stdout: %s stderr: %s", stdOut, stdErr)
				assert.Contains(t, stdErr, c.wantErr)
				return
			}

			require.NoError(t, err, "stderr: %s", stdErr)
			assertTrimmed(t, c.wantEnv, stdOut)
		})
	}
}
