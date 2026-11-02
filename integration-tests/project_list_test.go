package tests

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestProjectList(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"
	otherUserID := "other-user-id"
	vendor := "test-vendor"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg("org-id-1", "org-1", "Org 1", myUserID, "flexible"),
		makeOrg("org-id-2", "org-2", "Org 2", otherUserID, "fixed"),
	})
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject("project-id-1", "org-id-1", vendor, "Project 1", "region-1"),
		makeProject("project-id-2", "org-id-2", vendor, "Project 2", "region-2"),
		makeProject("project-id-3", "org-id-2", vendor, "Project 3", "region-2"),
		makeProject("project-other-vendor-3", "org-other-vendor", "acme", "Other Vendor's Project", "region-1"),
	})
	apiHandler.SetUserGrants([]*mockapi.UserGrant{
		{
			ResourceID:     "org-id-1",
			ResourceType:   "organization",
			OrganizationID: "org-id-1",
			UserID:         myUserID,
			Permissions:    []string{"admin"},
		},
		{
			ResourceID:     "project-id-1",
			ResourceType:   "project",
			OrganizationID: "org-id-1",
			UserID:         myUserID,
			Permissions:    []string{"admin"},
		},
		{
			ResourceID:     "project-id-2",
			ResourceType:   "project",
			OrganizationID: "org-id-2",
			UserID:         "user-id-2",
			Permissions:    []string{"admin"},
		},
		{
			ResourceID:     "project-id-2",
			ResourceType:   "project",
			OrganizationID: "org-id-2",
			UserID:         myUserID,
			Permissions:    []string{"viewer", "development:admin"},
		},
		{
			ResourceID:     "project-id-3",
			ResourceType:   "project",
			OrganizationID: "org-id-2",
			UserID:         myUserID,
			Permissions:    []string{"viewer", "development:contributor"},
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assertTrimmed(t, `
+--------------+-----------+----------+----------+----------+
| ID           | Title     | Region   | Org name | Org type |
+--------------+-----------+----------+----------+----------+
| project-id-1 | Project 1 | region-1 | org-1    | flexible |
| project-id-2 | Project 2 | region-2 | org-2    | fixed    |
| project-id-3 | Project 3 | region-2 | org-2    | fixed    |
+--------------+-----------+----------+----------+----------+
`, f.Run("pro", "-v"))

	assertTrimmed(t, `
ID	Title	Region	Org name	Org type
project-id-1	Project 1	region-1	org-1	flexible
project-id-2	Project 2	region-2	org-2	fixed
project-id-3	Project 3	region-2	org-2	fixed
`, f.Run("pro", "-v", "--format", "plain"))

	assertTrimmed(t, `
ID,Org ID
project-id-1,org-id-1
project-id-2,org-id-2
project-id-3,org-id-2
`, f.Run("pro", "-v", "--format", "csv", "--columns", "id,organization_id"))

	assertTrimmed(t, `
ID	Title	Region	Org name	Org type
project-id-1	Project 1	region-1	org-1	flexible
`, f.Run("pro", "-v", "--format", "plain", "--my"))

	assertTrimmed(t, `
ID	Title	Region	Org name	Org type
project-id-1	Project 1	region-1	org-1	flexible
`, f.Run("pro", "-v", "--format", "plain", "--org-type", "flexible"))

	assertTrimmed(t, `
project-id-1
project-id-2
project-id-3
`, f.Run("pro", "-v", "--pipe"))
}

func makeProject(id, org, vendor, title, region string) *mockapi.Project {
	return &mockapi.Project{
		ID:           id,
		Organization: org,
		Vendor:       vendor,
		Title:        title,
		Region:       region,
		Links:        mockapi.MakeHALLinks("self=/projects/" + url.PathEscape(id)),
	}
}

func TestProjectListCacheIsPerUser(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "user-1"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	apiHandler.SetOrgs([]*mockapi.Org{makeOrg("org-id-1", "org-1", "Org 1", "user-1", "flexible")})
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject("project-id-1", "org-id-1", "test-vendor", "Project 1", "region-1"),
	})
	apiHandler.SetUserGrants([]*mockapi.UserGrant{{
		ResourceID:     "project-id-1",
		ResourceType:   "project",
		OrganizationID: "org-id-1",
		UserID:         "user-1",
		Permissions:    []string{"admin"},
	}})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	assert.Equal(t, "project-id-1\n", f.Run("projects", "--pipe", "--refresh=0"))

	// Another user does not see the cached list.
	apiHandler.SetMyUser(&mockapi.User{ID: "user-2"})
	assert.Equal(t, "", f.Run("projects", "--pipe", "--refresh=0"))

	// Invalid credentials fail before the cache is read.
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=invalid")
	stdOut, _, err := f.RunCombinedOutput("projects", "--pipe", "--refresh=0")
	assert.Error(t, err)
	assert.Empty(t, stdOut)
}

func TestProjectListUnresolvedProjects(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg("org-id-1", "org-1", "Org 1", myUserID, "flexible"),
		makeOrg("org-id-acme", "org-acme", "Acme", myUserID, "flexible"),
	})
	// The mock returns a null reference for "project-deleted", which is not set.
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject("project-id-1", "org-id-1", "test-vendor", "Project 1", "region-1"),
		makeProject("project-no-vendor", "org-id-1", "", "No Vendor", "region-1"),
		makeProject("project-acme", "org-id-acme", "acme", "Acme Project", "region-1"),
	})
	grant := func(projectID, orgID string) *mockapi.UserGrant {
		return &mockapi.UserGrant{
			ResourceID:     projectID,
			ResourceType:   "project",
			OrganizationID: orgID,
			UserID:         myUserID,
			Permissions:    []string{"admin"},
		}
	}
	apiHandler.SetUserGrants([]*mockapi.UserGrant{
		grant("project-id-1", "org-id-1"),
		grant("project-no-vendor", "org-id-1"),
		grant("project-deleted", "org-id-1"),
		grant("project-acme", "org-id-acme"),
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	allIDs := []string{"project-id-1", "project-no-vendor", "project-deleted", "project-acme"}

	// If a reference request fails, projects are listed by ID, including those with an unknown vendor.
	apiHandler.FailNext("GET", "/ref/projects", http.StatusBadGateway, "<html>Bad Gateway</html>")
	stdOut, stdErr, err := f.RunCombinedOutput("pro", "--pipe", "-vvv")
	assert.NoError(t, err)
	assert.ElementsMatch(t, allIDs, strings.Fields(stdOut))
	assert.Contains(t, stdErr, "some project details could not be loaded")
	assert.Contains(t, stdErr, "Failed to load references: The API returned 502 Bad Gateway")
	assert.Contains(t, stdErr, "Missing details for 4/4 projects: ")
	assert.NotContains(t, stdErr, "You do not have any")

	// The incomplete list was not cached. A grant for a project that was not found is skipped, and projects with
	// a known vendor, or no vendor, are filtered as usual.
	stdOut, stdErr, err = f.RunCombinedOutput("pro", "--format", "csv", "--columns", "id,title,organization_name")
	assert.NoError(t, err)
	assert.Equal(t, "ID,Title,Org name\nproject-id-1,Project 1,org-1\n", stdOut)
	assert.NotContains(t, stdErr, "could not be loaded")

	_, stdErr, err = f.RunCombinedOutput("pro", "--refresh", "1", "-vvv")
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "Missing details for 1/4 projects: project-deleted")
	assert.Contains(t, stdErr, "Projects hidden by the vendor filter (test-vendor): ")
	assert.Contains(t, stdErr, "project-acme")
	assert.Contains(t, stdErr, "project-no-vendor")
	assert.NotContains(t, stdErr, "could not be loaded")

	// If organization details cannot be loaded, only projects with an unknown vendor are kept.
	apiHandler.FailNext("GET", "/ref/organizations", http.StatusBadGateway, "<html>Bad Gateway</html>")
	stdOut, stdErr, err = f.RunCombinedOutput("pro", "--pipe", "--refresh", "1")
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"project-id-1", "project-no-vendor"}, strings.Fields(stdOut))
	assert.Contains(t, stdErr, "some project details could not be loaded")

	// A reference response that is not JSON is handled in the same way.
	apiHandler.FailNext("GET", "/ref/projects", http.StatusOK, "<html>Sign in</html>")
	stdOut, stdErr, err = f.RunCombinedOutput("pro", "--pipe", "--refresh", "1")
	assert.NoError(t, err)
	assert.ElementsMatch(t, allIDs, strings.Fields(stdOut))
	assert.Contains(t, stdErr, "some project details could not be loaded")

	// The table renders projects without details.
	apiHandler.FailNext("GET", "/ref/projects", http.StatusBadGateway, "<html>Bad Gateway</html>")
	stdOut = f.Run("pro", "--refresh", "1", "--columns", "id,title,region,status,created_at")
	assert.Contains(t, stdOut, "project-deleted")
}
