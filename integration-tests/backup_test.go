package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestBackupList(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID:            projectID,
			DefaultBranch: "main",
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID,
				"environments=/projects/"+projectID+"/environments",
			),
		},
	})
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["backups"] = mockapi.HALLink{HREF: "/projects/" + projectID + "//environments/main/backups"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	created1, err := time.Parse(time.RFC3339, "2014-04-01T10:00:00+01:00")
	require.NoError(t, err)
	created2, err := time.Parse(time.RFC3339, "2015-04-01T10:00:00+01:00")
	require.NoError(t, err)

	apiHandler.SetProjectBackups(projectID, []*mockapi.Backup{
		{
			ID:            "123",
			EnvironmentID: "main",
			Status:        "CREATED",
			Safe:          true,
			Restorable:    true,
			Automated:     false,
			CommitID:      "foo",
			CreatedAt:     created1,
		},
		{
			ID:            "456",
			EnvironmentID: "main",
			Status:        "CREATED",
			Safe:          false,
			Restorable:    true,
			Automated:     true,
			CommitID:      "bar",
			CreatedAt:     created2,
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assertTrimmed(t, `
+---------------------------+-----------+------------+
| Created                   | Backup ID | Restorable |
+---------------------------+-----------+------------+
| 2015-04-01T09:00:00+00:00 | 456       | true       |
| 2014-04-01T09:00:00+00:00 | 123       | true       |
+---------------------------+-----------+------------+
`, f.Run("backups", "-p", projectID, "-e", "."))

	assertTrimmed(t, `
+---------------------------+-----------+------------+-----------+-----------+
| Created                   | Backup ID | Restorable | Automated | Commit ID |
+---------------------------+-----------+------------+-----------+-----------+
| 2015-04-01T09:00:00+00:00 | 456       | true       | true      | bar       |
| 2014-04-01T09:00:00+00:00 | 123       | true       | false     | foo       |
+---------------------------+-----------+------------+-----------+-----------+
`, f.Run("backups", "-p", projectID, "-e", ".", "--columns", "+automated,commit_id"))
}

func TestBackupCreate(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{
		{
			ID:            projectID,
			DefaultBranch: "main",
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID,
				"environments=/projects/"+projectID+"/environments",
			),
		},
	})
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["backups"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main//backups"}
	main.Links["#backup"] = mockapi.HALLink{HREF: "/projects/" + projectID + "/environments/main/backups"}
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	f.Run("backup", "-p", projectID, "-e", ".")

	assert.NotEmpty(t, f.Run("backups", "-p", projectID, "-e", "."))
}

// setUpBackups serves a project with "main" and "staging" environments, and backups of "main".
func setUpBackups(t *testing.T) (apiHandler *mockapi.Handler, f *cmdFactory, projectID string) {
	apiHandler = mockapi.NewHandler(t)
	projectID = mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:            projectID,
		DefaultBranch: "main",
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		Settings: map[string]any{"sizing_api_enabled": true},
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
		makeEnv(projectID, "staging", "staging", "active", "main"),
	})
	created1, err := time.Parse(time.RFC3339, "2014-04-01T10:00:00Z")
	require.NoError(t, err)
	created2, err := time.Parse(time.RFC3339, "2015-04-01T10:00:00Z")
	require.NoError(t, err)
	apiHandler.SetProjectBackups(projectID, []*mockapi.Backup{
		{ID: "123", EnvironmentID: "main", Status: "CREATED", Restorable: true, CommitID: "foo", CreatedAt: created1},
		{ID: "456", EnvironmentID: "main", Status: "CREATED", Restorable: true, CommitID: "bar", CreatedAt: created2},
		{ID: "789", EnvironmentID: "main", Status: "CREATED", Restorable: false, CreatedAt: created1},
	})

	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)
	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)
	f = newCommandFactory(t, apiServer.URL, authServer.URL)

	return apiHandler, f, projectID
}

func TestBackupGet(t *testing.T) {
	t.Parallel()
	_, f, projectID := setUpBackups(t)

	assertTrimmed(t, "foo", f.Run("backup:get", "-p", projectID, "-e", "main", "123", "-P", "commit_id"))

	// The most recent backup is the default.
	assertTrimmed(t, "456", f.Run("backup:get", "-p", projectID, "-e", "main", "-P", "id"))

	_, stderr, err := f.RunCombinedOutput("backup:get", "-p", projectID, "-e", "main", "000")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Backup not found: 000")

	_, stderr, err = f.RunCombinedOutput("backup:get", "-p", projectID, "-e", "staging")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "No backups found.")
}

func TestBackupDelete(t *testing.T) {
	t.Parallel()
	apiHandler, f, projectID := setUpBackups(t)

	_, stderr, err := f.RunCombinedOutput("backup:delete", "-p", projectID, "-e", "main")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "A backup ID is required in non-interactive mode.")

	_, stderr, err = f.RunCombinedOutput("backup:delete", "-p", projectID, "-e", "main", "000")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Backup not found: 000")

	_, stderr, err = f.RunCombinedOutput("backup:delete", "-p", projectID, "-e", "main", "123")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "The backup 123 (2014-04-01T10:00:00+00:00) has been deleted.")
	assert.Equal(t, []string{"456", "789"}, apiHandler.ProjectBackupIDs(projectID))
	assert.Equal(t, "environment.backup.delete", apiHandler.ProjectActivities(projectID)[0].Type)

	// Choose a backup interactively, then decline the confirmation.
	_, stderr, err = f.RunInteractive("0\nn\n", "backup:delete", "-p", projectID, "-e", "main")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Are you sure you want to delete the backup 456")
	assert.Equal(t, []string{"456", "789"}, apiHandler.ProjectBackupIDs(projectID))
}

func TestBackupRestore(t *testing.T) {
	t.Parallel()
	apiHandler, f, projectID := setUpBackups(t)

	// The API sizing feature adds the --no-resources option.
	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := strings.Replace(string(baseConfig), "\napi:\n", "\napi:\n  sizing: true\n", 1)
	require.NoError(t, os.WriteFile(configFile, []byte(config), 0o600))
	f.useConfig(configFile)

	cases := []struct {
		name       string
		args       []string
		wantStderr []string
		wantBody   map[string]any
	}{
		{
			name:       "most recent",
			wantStderr: []string{"Backup ID: 456", "Restoring backup 456 to main"},
			wantBody:   map[string]any{"environment_name": "main"},
		},
		{
			name: "no code or resources to a new environment",
			args: []string{"123", "--target", "new-env", "--branch-from", "main", "--no-code", "--no-resources"},
			wantStderr: []string{
				"Backup ID: 123",
				"Only data, not code, will be restored.",
				"Original environment: main",
				"Restoring backup 123 to new-env",
			},
			wantBody: map[string]any{
				"environment_name":  "new-env",
				"branch_from":       "main",
				"restore_code":      false,
				"restore_resources": false,
			},
		},
		{
			name:       "resources init",
			args:       []string{"123", "--target", "staging", "--resources-init", "minimum"},
			wantStderr: []string{"Restoring backup 123 to staging"},
			wantBody: map[string]any{
				"environment_name": "staging",
				"resources":        map[string]any{"init": "minimum"},
			},
		},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"backup:restore", "-p", projectID, "-e", "main"}, c.args...)
			_, stderr, err := f.RunCombinedOutput(args...)
			require.NoError(t, err, stderr)
			for _, s := range c.wantStderr {
				assert.Contains(t, stderr, s)
			}
			assert.Contains(t, stderr, "The activity succeeded")
			restores := apiHandler.BackupRestores(projectID)
			require.Len(t, restores, i+1)
			assert.Equal(t, c.wantBody, restores[i])
		})
	}

	t.Run("errors", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput("backup:restore", "-p", projectID, "-e", "main", "789")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "The backup 789 cannot be restored")

		_, stderr, err = f.RunCombinedOutput("backup:restore", "-p", projectID, "-e", "main", "000")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "Backup not found: 000")

		_, stderr, err = f.RunCombinedOutput("backup:restore", "-p", projectID, "-e", "main", "123",
			"--target", "new-env", "--branch-from", "missing")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "Environment not found (in --branch-from): missing")

		assert.Len(t, apiHandler.BackupRestores(projectID), len(cases))
	})
}
