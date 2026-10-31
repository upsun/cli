package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// listCase is a list command run, and its expected stdout or error.
type listCase struct {
	name    string
	args    []string
	want    string
	wantErr string
}

func runListCases(t *testing.T, f *cmdFactory, cases []listCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			stdout, stderr, err := f.RunCombinedOutput(c.args...)
			if c.wantErr != "" {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, c.wantErr)
				return
			}
			require.NoError(t, err, stderr)
			assert.Equal(t, c.want, stdout)
		})
	}
}

func TestEnvironmentListOptions(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID:    projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID, "environments=/projects/"+projectID+"/environments"),
	}})
	envs := []*mockapi.Environment{
		makeEnv(projectID, "main", "production", "active", nil),
		makeEnv(projectID, "staging", "staging", "active", "main"),
		makeEnv(projectID, "dev-2", "development", "inactive", "main"),
		makeEnv(projectID, "dev-10", "development", "active", "main"),
	}
	envs[0].Title = "Production"
	envs[1].Title = "b-stage"
	envs[2].Title = "A-dev"
	envs[3].Title = "c-dev"
	apiHandler.SetEnvironments(envs)

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	list := func(args ...string) []string {
		return append([]string{"environment:list", "-p", projectID, "--pipe"}, args...)
	}

	runListCases(t, f, []listCase{
		{name: "default sort by title", args: list(), want: "dev-2\nstaging\ndev-10\nmain\n"},
		{name: "sort by id", args: list("--sort", "id"), want: "dev-2\ndev-10\nmain\nstaging\n"},
		{name: "reverse", args: list("--sort", "id", "--reverse"), want: "staging\nmain\ndev-10\ndev-2\n"},
		{name: "reverse default sort", args: list("--reverse"), want: "main\ndev-10\nstaging\ndev-2\n"},
		{name: "sort by status", args: list("--sort", "status"), want: "dev-10\nmain\nstaging\ndev-2\n"},
		{name: "unsorted", args: list("--sort", ""), want: "dev-10\ndev-2\nmain\nstaging\n"},
		{name: "type", args: list("--type", "development"), want: "dev-2\ndev-10\n"},
		{name: "types", args: list("--type", "production,staging"), want: "staging\nmain\n"},
		{name: "unknown type", args: list("--type", "qa"), want: ""},
		{name: "type and status", args: list("--type", "development", "--status", "active"), want: "dev-10\n"},
		{name: "unknown sort property", args: list("--sort", "nope"), wantErr: "Cannot sort: property not found: nope"},
		{
			name:    "unknown sort property with one item",
			args:    list("--type", "production", "--sort", "nope"),
			wantErr: "Cannot sort: property not found: nope",
		},
	})

	// The list is cached unless refreshed.
	count := func() int { return len(apiHandler.Requests("GET", "/projects/*/environments")) }
	before := count()
	f.Run(list("--refresh", "0")...)
	assert.Equal(t, before, count())
	f.Run(list()...)
	assert.Equal(t, before+1, count())
}

func TestProjectListOptions(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"
	// Values of --org that look like an organization ID (a ULID) are matched against IDs.
	orgID1 := "01H8Z9Y7X6W5V4T3S2R1Q0P9N8"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg(orgID1, "org-1", "Org 1", myUserID, "flexible"),
		makeOrg("org-id-2", "org-2", "Org 2", "other-user-id", "fixed"),
	})
	apiHandler.SetProjects([]*mockapi.Project{
		makeProject("project-a", orgID1, "test-vendor", "Shop 10", "eu-1.example.com"),
		makeProject("project-b", "org-id-2", "test-vendor", "shop 2", "us-1.example.com"),
		makeProject("project-c", "org-id-2", "test-vendor", "Blog", "eu-1.example.com"),
	})
	grants := make([]*mockapi.UserGrant, 0, 3)
	for _, p := range []struct{ id, org string }{{"project-a", orgID1}, {"project-b", "org-id-2"}, {"project-c",
		"org-id-2"}} {
		grants = append(grants, &mockapi.UserGrant{
			ResourceID: p.id, ResourceType: "project", OrganizationID: p.org, UserID: myUserID, Permissions: []string{"admin"},
		})
	}
	apiHandler.SetUserGrants(grants)

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	list := func(args ...string) []string {
		return append([]string{"project:list", "--pipe"}, args...)
	}

	runListCases(t, f, []listCase{
		{name: "default sort by title", args: list(), want: "project-c\nproject-b\nproject-a\n"},
		{name: "reverse", args: list("--reverse"), want: "project-a\nproject-b\nproject-c\n"},
		{name: "sort by id", args: list("--sort", "id"), want: "project-a\nproject-b\nproject-c\n"},
		{name: "sort by region", args: list("--sort", "region", "--reverse"), want: "project-b\nproject-c\nproject-a\n"},
		{name: "title", args: list("--title", "SHOP"), want: "project-b\nproject-a\n"},
		{name: "title not found", args: list("--title", "wiki"), want: ""},
		{name: "region", args: list("--region", "EU-1.example.com"), want: "project-c\nproject-a\n"},
		{name: "partial region", args: list("--region", "eu-1"), want: ""},
		{name: "org by name", args: list("--org", "org-2"), want: "project-c\nproject-b\n"},
		{name: "org by ID", args: list("--org", orgID1), want: "project-a\n"},
		{name: "unknown org", args: list("--org", "org-3"), want: ""},
		{name: "unknown sort property", args: list("--sort", "nope"), wantErr: "Cannot sort: property not found: nope"},
		{
			name:    "unknown sort property with one item",
			args:    list("--title", "Blog", "--sort", "nope"),
			wantErr: "Cannot sort: property not found: nope",
		},
	})
}

func TestOrganizationListOptions(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "user-id-1"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg("org-id-1", "acme", "ACME 10", myUserID, "flexible"),
		makeOrg("org-id-2", "beta", "acme 2", myUserID, "fixed"),
		makeOrg("org-id-3", "duff", "Duff Beer", "user-id-2", "fixed"),
	})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	list := func(args ...string) []string {
		return append([]string{"organization:list", "--format", "plain", "--no-header", "--columns", "name"}, args...)
	}

	runListCases(t, f, []listCase{
		{name: "default", args: list(), want: "acme\nbeta\nduff\n"},
		{name: "sort by label", args: list("--sort", "label"), want: "beta\nacme\nduff\n"},
		{name: "reverse", args: list("--sort", "name", "--reverse"), want: "duff\nbeta\nacme\n"},
		{name: "my", args: list("--my"), want: "acme\nbeta\n"},
		{name: "type", args: list("--type", "fixed"), want: "beta\nduff\n"},
		{name: "my and type", args: list("--my", "--type", "fixed"), want: "beta\n"},
		{name: "unknown sort property", args: list("--sort", "nope"), wantErr: "Cannot sort: property not found: nope"},
		{
			name:    "unknown sort property with one item",
			args:    list("--type", "flexible", "--sort", "nope"),
			wantErr: "Cannot sort: property not found: nope",
		},
	})
}
