package tests

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentDeploy(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID: projectID,
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID,
				"environments=/projects/"+projectID+"/environments",
			),
			DefaultBranch: "main",
		},
	})
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["#activities"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/activities"}
	main.Links["#deploy"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/deploy"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	created1, _ := time.Parse(time.RFC3339, "2014-04-01T10:00:00Z")
	created2, _ := time.Parse(time.RFC3339, "2014-04-02T10:00:00Z")
	updated, _ := time.Parse(time.RFC3339, "2014-04-02T11:00:00Z")
	apiHandler.SetProjectActivities(projectID, []*mockapi.Activity{
		{
			ID:                "act1",
			Type:              "environment.push",
			State:             "staged",
			Result:            "success",
			CompletionPercent: 100,
			Project:           projectID,
			Environments:      []string{"main"},
			Description:       "<user>Mock User</user> pushed to <environment>main</environment>",
			Text:              "Mock User pushed to main",
			CreatedAt:         created1,
			UpdatedAt:         updated,
		}, {
			ID:                "act2",
			Type:              "environment.variable.create",
			State:             "staged",
			Result:            "success",
			CompletionPercent: 100,
			Project:           projectID,
			Environments:      []string{"main"},
			Description:       "<user>Mock User</user> created variable <variable>X</variable> on environment <environment>main</environment>", //nolint:lll
			Text:              "Mock User created variable X on environment main",
			CreatedAt:         created2,
			UpdatedAt:         updated,
		},
	})

	assertTrimmed(t, `
+------+--------------------------+---------------------------------+-----------------------------+---------+
| ID   | Created                  | Description                     | Type                        | Result  |
+------+--------------------------+---------------------------------+-----------------------------+---------+
| act2 | 2014-04-02T10:00:00+00:0 | Mock User created variable X on | environment.variable.create | success |
|      | 0                        | environment main                |                             |         |
| act1 | 2014-04-01T10:00:00+00:0 | Mock User pushed to main        | environment.push            | success |
|      | 0                        |                                 |                             |         |
+------+--------------------------+---------------------------------+-----------------------------+---------+
`, f.Run("env:deploy", "-p", projectID, "-e", "main"))
}

func TestEnvironmentDeployStrategy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		rolling      bool
		staged       bool
		args         []string
		wantErr      bool
		wantStdErr   string
		wantStrategy string
	}{
		{name: "default", staged: true, wantStrategy: "stopstart"},
		{name: "stopstart", staged: true, args: []string{"--strategy", "stopstart"}, wantStrategy: "stopstart"},
		{
			name: "rolling", rolling: true, staged: true, args: []string{"--strategy", "rolling"},
			wantStdErr:   "Please make sure the changes from above are not affecting the state of the services.",
			wantStrategy: "rolling",
		},
		{
			name: "rolling unavailable", staged: true, args: []string{"--strategy", "rolling"},
			wantErr: true, wantStdErr: "The chosen strategy is not available for this environment.",
		},
		{
			name: "invalid", rolling: true, staged: true, args: []string{"--strategy", "fast"},
			wantErr: true, wantStdErr: "The chosen strategy is not available for this environment.",
		},
		{name: "no staged changes", wantStdErr: "The environment main (type: production) has no staged changes to deploy."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			main := makeEnv(layoutProjectID, "main", "production", "active", nil)
			main.AddOperations("deploy", "activities")
			main.CanRollingDeploy = c.rolling
			apiHandler, f, _ := setUpProject(t, main)
			if c.staged {
				apiHandler.SetProjectActivities(layoutProjectID, []*mockapi.Activity{{
					ID:           "staged1",
					Type:         "environment.variable.create",
					State:        "staged",
					Project:      layoutProjectID,
					Environments: []string{"main"},
					Description:  "Mock User created a variable",
					CreatedAt:    time.Now().Add(-time.Hour),
				}})
			}

			args := append([]string{"environment:deploy", "-p", layoutProjectID, "-e", "main"}, c.args...)
			_, stdErr, err := f.RunCombinedOutput(args...)
			if c.wantErr {
				assertExitCode(t, 1, err)
			} else {
				require.NoError(t, err)
			}
			assert.Contains(t, stdErr, c.wantStdErr)
			actions := apiHandler.EnvironmentActions(layoutProjectID, "main")
			if c.wantStrategy == "" {
				assert.Empty(t, actions)
				return
			}
			require.Len(t, actions, 1)
			assert.Equal(t, mockapi.EnvironmentAction{
				Type: "environment.deploy",
				Body: map[string]any{"strategy": c.wantStrategy},
			}, actions[0])
		})
	}
}
