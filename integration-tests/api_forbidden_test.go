package tests

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestForbiddenClearsCache checks that a 403 response clears the cached project and environments, as access may
// have changed.
func TestForbiddenClearsCache(t *testing.T) {
	t.Parallel()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: selectionUserID})
	p := makeProject(layoutProjectID, selectionOrgID, "test-vendor", "Main", "region-1")
	p.DefaultBranch = "main"
	p.Links = mockapi.MakeHALLinks("self=/projects/"+layoutProjectID,
		"environments=/projects/"+layoutProjectID+"/environments")
	apiHandler.SetProjects([]*mockapi.Project{p})
	env := makeEnv(layoutProjectID, "main", "production", "active", nil)
	env.Links["#variables"] = mockapi.HALLink{HREF: "/projects/" + layoutProjectID + "/environments/main/variables"}
	apiHandler.SetEnvironments([]*mockapi.Environment{env})
	forbidden := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":"error","code":403,"message":"Forbidden"}`))
	}
	apiHandler.Get("/projects/"+layoutProjectID+"/environments/main/variables", forbidden)
	apiHandler.Get("/projects/"+layoutProjectID+"/variables", forbidden)
	f := serveAPI(t, apiHandler)

	countRequests := func(path string) int { return len(apiHandler.Requests("GET", path)) }
	projectPath := "/projects/" + layoutProjectID
	envListPath := projectPath + "/environments"
	envInfo := func() { assert.Equal(t, "main\n", f.Run("env:info", "-p", layoutProjectID, "-e", "main", "id")) }

	// The project and environments are cached.
	envInfo()
	projects, envLists := countRequests(projectPath), countRequests(envListPath)
	envInfo()
	require.Equal(t, projects, countRequests(projectPath))
	require.Equal(t, envLists, countRequests(envListPath))

	// A 403 on an environment's resource clears the cached environments.
	_, _, err := f.RunCombinedOutput("variable:list", "-p", layoutProjectID, "-e", "main", "-l", "environment")
	require.Error(t, err)
	envLists = countRequests(envListPath)
	envInfo()
	assert.Equal(t, envLists+1, countRequests(envListPath))

	// A 403 on a project's resource clears the cached project.
	_, _, err = f.RunCombinedOutput("variable:list", "-p", layoutProjectID, "-l", "project")
	require.Error(t, err)
	projects = countRequests(projectPath)
	envInfo()
	assert.Equal(t, projects+1, countRequests(projectPath))
}
