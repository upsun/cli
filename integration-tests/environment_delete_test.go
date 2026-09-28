package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestEnvironmentDelete(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
		makeEnv(projectID, "test-1", "development", "active", "main"),
		makeEnv(projectID, "test-2", "development", "active", "main"),
		makeEnv(projectID, "dev", "development", "active", "main"),
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	cases := []struct {
		name        string
		args        []string
		extraEnv    []string
		wantErr     bool
		wantStdErr  []string
		wantMissing []string
	}{
		{
			name:        "single wildcard argument",
			args:        []string{"test-*"},
			wantStdErr:  []string{"2 environments found by ID.", "Selected environments: test-1, test-2"},
			wantMissing: []string{"Specified environment not found"},
		},
		{
			name:        "wildcard option",
			args:        []string{"-e", "test-*"},
			wantStdErr:  []string{"2 environments found by ID.", "Selected environments: test-1, test-2"},
			wantMissing: []string{"Specified environment not found"},
		},
		{
			name:        "ignores an unknown branch variable",
			args:        []string{"--type", "development"},
			extraEnv:    []string{"PLATFORM_BRANCH=missing"},
			wantStdErr:  []string{"3 environments found matching type(s): development"},
			wantMissing: []string{"Specified environment not found"},
		},
		{
			name:        "missing environment",
			args:        []string{"missing"},
			wantErr:     true,
			wantStdErr:  []string{"Environment not found: missing", "0 environments found by ID."},
			wantMissing: []string{"Specified environment not found"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f.extraEnv = c.extraEnv
			args := append([]string{"environment:delete", "-p", projectID}, c.args...)
			// Decline any confirmation, so nothing is deleted.
			_, stdErr, err := f.RunInteractive("n\nn\n", args...)
			if c.wantErr {
				assert.Error(t, err)
			}
			for _, s := range c.wantStdErr {
				assert.Contains(t, stdErr, s)
			}
			for _, s := range c.wantMissing {
				assert.NotContains(t, stdErr, s)
			}
		})
	}
}
