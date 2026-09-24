package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestSSHBrokenEnv checks SSH URL selection on an active environment whose
// current deployment cannot be loaded.
func TestSSHBrokenEnv(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	projectID := mockapi.ProjectID()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	// No current deployment is set, so the API returns 404 for it.
	mainEnv := makeEnv(projectID, "main", "production", "active", nil)
	mainEnv.Links["pf:ssh:app"] = mockapi.HALLink{HREF: "ssh://app@ssh.cli-tests.example.com"}
	mainEnv.Links["pf:ssh:other"] = mockapi.HALLink{HREF: "ssh://other@ssh.cli-tests.example.com"}
	apiHandler.SetEnvironments([]*mockapi.Environment{mainEnv})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	cases := []struct {
		name     string
		args     []string
		extraEnv []string
		want     string
		wantErr  string
	}{
		{name: "no app", wantErr: "No SSH URL found for environment 'main'"},
		{name: "app option", args: []string{"--app", "other"}, want: "other@ssh.cli-tests.example.com"},
		{
			name:     "app from environment variable",
			extraEnv: []string{"PLATFORM_APPLICATION_NAME=other"},
			want:     "other@ssh.cli-tests.example.com",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			f.extraEnv = c.extraEnv
			args := append([]string{"ssh", "-p", projectID, "-e", "main", "--pipe"}, c.args...)
			stdOut, stdErr, err := f.RunCombinedOutput(args...)
			if c.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, stdErr, c.wantErr)
				return
			}
			require.NoError(t, err, "stderr: %s", stdErr)
			assert.Equal(t, c.want, stdOut)
		})
	}
}
