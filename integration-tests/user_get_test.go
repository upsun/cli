package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestUserGet covers user:get with missing optional inputs, which previously
// caused TypeErrors in the legacy CLI.
func TestUserGet(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	projectID := mockapi.ProjectID()
	myUserID := "my-user-id"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg("org-id-1", "org-1", "Org 1", myUserID, "flexible"),
	})
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject(projectID, "org-id-1", "test-vendor", "Project 1", "region-1"),
	})
	apiHandler.SetUserGrants([]*mockapi.UserGrant{
		{
			ResourceID:     projectID,
			ResourceType:   "project",
			OrganizationID: "org-id-1",
			UserID:         "user-id-2",
			Permissions:    []string{"viewer", "development:viewer"},
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	cases := []struct {
		name       string
		args       []string
		wantErr    bool
		wantStdout string
		wantStderr string
	}{
		{
			name:       "pipe without level",
			args:       []string{"user:get", "-p", projectID, "user-id-2@example.com", "--pipe"},
			wantStdout: "viewer",
		},
		{
			name:       "invalid level",
			args:       []string{"user:get", "-p", projectID, "user-id-2@example.com", "--level", "foo"},
			wantErr:    true,
			wantStderr: "Invalid level: foo",
		},
		{
			name:       "no email in non-interactive mode",
			args:       []string{"user:get", "-p", projectID},
			wantErr:    true,
			wantStderr: "An email address is required (in non-interactive mode).",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := f.RunCombinedOutput(c.args...)
			if c.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "stderr: %s", stderr)
			}
			assert.NotContains(t, stderr, "TypeError")
			assert.Equal(t, c.wantStdout, strings.TrimSpace(stdout))
			assert.Contains(t, stderr, c.wantStderr)
		})
	}
}
