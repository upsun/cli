package tests

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestProjectClearBuildCache(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"#clear-build-cache=/projects/"+projectID+"/clear_build_cache",
		),
	}})
	f := serveAPI(t, apiHandler)

	_, stderr, err := f.RunCombinedOutput("project:clear-build-cache", "-p", projectID)
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "The build cache has been cleared on the project: "+projectID)
	activities := apiHandler.ProjectActivities(projectID)
	require.Len(t, activities, 1)
	assert.Equal(t, "project.clear_build_cache", activities[0].Type)
}

// setUpProjectDelete serves a project, owned by another user, and its subscription.
// It returns a function that reports whether the subscription was deleted.
func setUpProjectDelete(t *testing.T, deleteStatus int) (f *cmdFactory, projectID string, deleted func() bool) {
	apiHandler := mockapi.NewHandler(t)
	orgID := "org-id"
	apiHandler.SetOrgs([]*mockapi.Org{{
		ID: orgID, Type: "flexible", Name: "acme", Label: "Acme", Owner: "my-user-id",
		Links: mockapi.MakeHALLinks("self=/organizations/" + orgID),
	}})
	projectID = mockapi.ProjectID()
	subscriptionID := "123456"
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:           projectID,
		Title:        "My Project",
		Organization: orgID,
		Owner:        "other-user-id",
		Links:        mockapi.MakeHALLinks("self=/projects/" + projectID),
		Subscription: mockapi.ProjectSubscriptionInfo{LicenseURI: "/licenses/" + subscriptionID},
	}})

	var subscriptionDeleted atomic.Bool
	for _, path := range []string{
		"/subscriptions/" + subscriptionID,
		"/organizations/" + orgID + "/subscriptions/" + subscriptionID,
	} {
		apiHandler.Get(path, func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         subscriptionID,
				"project_id": projectID,
				"status":     "active",
				"_links":     mockapi.MakeHALLinks("self=" + path),
			})
		})
		apiHandler.Delete(path, func(w http.ResponseWriter, _ *http.Request) {
			if deleteStatus == http.StatusNoContent {
				subscriptionDeleted.Store(true)
				w.WriteHeader(deleteStatus)
				return
			}
			w.WriteHeader(deleteStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": deleteStatus,
				"title":  http.StatusText(deleteStatus),
				"detail": "you do not have permission to access this subscription or it does not exist",
			})
		})
	}

	return serveAPI(t, apiHandler), projectID, subscriptionDeleted.Load
}

func TestProjectDelete(t *testing.T) {
	t.Parallel()

	t.Run("not confirmed", func(t *testing.T) {
		t.Parallel()
		f, projectID, deleted := setUpProjectDelete(t, http.StatusNoContent)

		// The confirmation defaults to "no".
		_, stderr, err := f.RunCombinedOutput("project:delete", projectID)
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "You are about to delete the project:")

		_, stderr, err = f.RunInteractive("y\nWrong title\n", "project:delete", projectID)
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "Incorrect project title (expected: My Project)")

		_, stderr, err = f.RunCombinedOutput("project:delete", projectID, "-p", projectID)
		assert.Error(t, err)
		assert.Contains(t, stderr, "You cannot use both the <project> argument and the --project option")

		assert.False(t, deleted())
	})

	t.Run("interactive", func(t *testing.T) {
		t.Parallel()
		f, projectID, deleted := setUpProjectDelete(t, http.StatusNoContent)

		_, stderr, err := f.RunInteractive("y\nMy Project\n", "project:delete", projectID)
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "The project My Project ("+projectID+") was deleted.")
		assert.True(t, deleted())
	})

	t.Run("yes", func(t *testing.T) {
		t.Parallel()
		f, projectID, deleted := setUpProjectDelete(t, http.StatusNoContent)

		_, stderr, err := f.RunCombinedOutput("project:delete", "-p", projectID, "--yes")
		require.NoError(t, err, stderr)
		assert.Contains(t, stderr, "was deleted.")
		assert.True(t, deleted())
	})

	t.Run("not the owner", func(t *testing.T) {
		t.Parallel()
		f, projectID, deleted := setUpProjectDelete(t, http.StatusForbidden)

		// The owner check only applies without the Organizations API.
		baseConfig, err := os.ReadFile("config.yaml")
		require.NoError(t, err)
		configFile := filepath.Join(t.TempDir(), "config.yaml")
		config := strings.Replace(string(baseConfig), "  organizations: true\n", "  organizations: false\n", 1)
		require.NoError(t, os.WriteFile(configFile, []byte(config), 0o600))
		f.useConfig(configFile)

		_, stderr, err := f.RunCombinedOutput("project:delete", "-p", projectID, "--yes")
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "Only the project's owner can delete it.")
		assert.False(t, deleted())
	})
}
