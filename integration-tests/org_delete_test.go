package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestOrgDelete(t *testing.T) {
	t.Parallel()
	myUserID := "my-user-id"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg("org-id-1", "org-1", "Org 1", myUserID, "flexible"),
		makeOrg("org-id-2", "org-2", "Org 2", myUserID, "flexible"),
	})
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:             mockapi.ProjectID(),
		Title:          "Project 1",
		Organization:   "org-id-1",
		SubscriptionID: "1001",
	}})
	f := serveAPI(t, apiHandler)

	_, stderr, err := f.RunCombinedOutput("organization:delete", "-o", "org-1", "--yes")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "The organization Org 1 (org-1) still owns project(s), so it cannot be deleted.")
	assert.Contains(t, stderr, "To list the organization's projects, run: platform-test org:subscriptions")

	// The confirmation defaults to "no".
	_, stderr, err = f.RunCombinedOutput("organization:delete", "-o", "org-2")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "Are you sure you want to delete the organization Org 2 (org-2)?")
	assert.Equal(t, []string{"org-id-1", "org-id-2"}, apiHandler.OrgIDs())

	_, stderr, err = f.RunCombinedOutput("organization:delete", "-o", "org-2", "--yes")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "The organization Org 2 (org-2) was deleted.")
	assert.Equal(t, []string{"org-id-1"}, apiHandler.OrgIDs())
}

func TestOrgSubscriptionList(t *testing.T) {
	t.Parallel()
	myUserID := "my-user-id"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg("org-id-1", "org-1", "Org 1", myUserID, "flexible"),
		makeOrg("org-id-2", "org-2", "Org 2", myUserID, "flexible"),
	})
	apiHandler.SetProjects([]*mockapi.Project{
		{ID: "project1", Title: "Project 1", Region: "region-1", Organization: "org-id-1", SubscriptionID: "1001"},
		{ID: "project2", Title: "Project 2", Region: "region-2", Organization: "org-id-1", SubscriptionID: "1002"},
		{ID: "project3", Title: "Project 3", Region: "region-1", Organization: "org-id-1", SubscriptionID: "1003"},
	})
	f := serveAPI(t, apiHandler)

	assertTrimmed(t, `
Subscription ID,Project ID,Title,Region
1001,project1,Project 1,region-1
1002,project2,Project 2,region-2
1003,project3,Project 3,region-1
`, f.Run("org:subs", "-o", "org-1", "--format", "csv"))

	// Pages.
	stdout, stderr, err := f.RunCombinedOutput("org:subs", "-o", "org-1", "--format", "csv", "--count", "2")
	require.NoError(t, err, stderr)
	assertTrimmed(t, "Subscription ID,Project ID,Title,Region\n1001,project1,Project 1,region-1\n"+
		"1002,project2,Project 2,region-2", stdout)

	assertTrimmed(t, "Subscription ID,Project ID,Title,Region\n1003,project3,Project 3,region-1",
		f.Run("org:subs", "-o", "org-1", "--format", "csv", "--count", "2", "--page", "2"))

	_, stderr, err = f.RunCombinedOutput("org:subs", "-o", "org-1", "--count", "2")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "Subscriptions belonging to the organization Org 1 (org-1) (page 1)")
	assert.Contains(t, stderr, "More subscriptions are available on the next page (--page 2)")

	assertTrimmed(t, "1001\n1002\n1003", f.Run("org:subs", "-o", "org-1", "--format", "plain", "--no-header",
		"--columns", "id", "--count", "1", "-c0"))

	_, stderr, err = f.RunCombinedOutput("org:subs", "-o", "org-2")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "No subscriptions were found belonging to the organization Org 2 (org-2).")

	_, stderr, err = f.RunCombinedOutput("org:subs", "-o", "org-1", "--count", "101")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "The --count must be a number between 1 and 100")
}
