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
