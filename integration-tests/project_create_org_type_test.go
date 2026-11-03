package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestProjectCreateOrgType(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	makeCreatableOrg := func(name, label, typ string) *mockapi.Org {
		org := makeOrg(name+"-id", name, label, "my-user-id", typ)
		org.Links["create-subscription"] = mockapi.HALLink{HREF: "/organizations/" + name + "-id/subscriptions"}
		return org
	}
	apiHandler.SetOrgs([]*mockapi.Org{
		makeCreatableOrg("flex-org", "Flex Org", "flexible"),
		makeCreatableOrg("fixed-org", "Fixed Org", "fixed"),
		makeCreatableOrg("fixed-two", "Fixed Two", "fixed"),
	})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	createArgs := []string{"project:create", "--title", "Test", "--region", "test-region"}

	// writeFiles creates a Git repository containing the given (empty) files.
	writeFiles := func(t *testing.T, files ...string) string {
		dir := initGitRepo(t)
		for _, name := range files {
			p := filepath.Join(dir, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.WriteFile(p, nil, 0o644))
		}
		return dir
	}

	t.Run("specified org with matching type", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput(append(createArgs, "--org", "flex-org", "--type", "flexible")...)
		require.NoError(t, err)
		assert.Contains(t, stderr, "Creating a project under the organization Flex Org (flex-org) (type: flexible)")
	})

	t.Run("specified org with another type", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput(append(createArgs, "--org", "flex-org", "--type", "fixed")...)
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "The organization Flex Org (flex-org) has the type flexible, not fixed.")
	})

	t.Run("invalid type", func(t *testing.T) {
		_, stderr, err := f.RunCombinedOutput(append(createArgs, "--org", "flex-org", "--type", "other")...)
		assertExitCode(t, 1, err)
		assert.Contains(t, stderr, "Invalid organization type: other")
	})

	t.Run("one org of the type is selected by default", func(t *testing.T) {
		_, stderr, err := f.RunInteractive("\n\n", append(createArgs, "--type", "flexible", "--no-set-remote")...)
		require.NoError(t, err)
		assert.Contains(t, stderr, "Selected organization: Flex Org (flex-org) (type: flexible) (by default)")
	})

	t.Run("chooser is filtered by type", func(t *testing.T) {
		_, stderr, err := f.RunInteractive("1\n\n\n", append(createArgs, "--type", "fixed", "--no-set-remote")...)
		require.NoError(t, err)
		assert.Contains(t, stderr, "Fixed Org (fixed-org) (type: fixed)")
		assert.Contains(t, stderr, "Fixed Two (fixed-two) (type: fixed)")
		assert.NotContains(t, stderr, "Flex Org")
	})

	mismatch := "The repository contains flexible configuration, " +
		"but the organization Fixed Org (fixed-org) has the type fixed."
	cases := []struct {
		name         string
		files        []string
		org          string
		stdin        string
		wantMismatch bool
		wantErr      bool
	}{
		{name: "flex config, fixed org, declined", files: []string{".upsun/config.yaml"}, org: "fixed-org",
			stdin: "n\n", wantMismatch: true, wantErr: true},
		{name: "flex config, fixed org, accepted", files: []string{".upsun/config.yaml"}, org: "fixed-org",
			stdin: "y\n\n\n", wantMismatch: true},
		{name: "fixed config, flex org", files: []string{".platform.app.yaml"}, org: "flex-org", stdin: "n\n",
			wantErr: true},
		{name: "flex config, flex org", files: []string{".upsun/config.yaml"}, org: "flex-org", stdin: "\n\n"},
		{name: "fixed config, fixed org", files: []string{".platform/routes.yaml"}, org: "fixed-org", stdin: "\n\n"},
		{name: "nested fixed app config, flex org", files: []string{"app/.platform.app.yaml"}, org: "flex-org",
			stdin: "n\n", wantErr: true},
		{name: "hidden directories are skipped", files: []string{".venv/app/.platform.app.yaml"}, org: "flex-org",
			stdin: "\n\n"},
		// The .platform directory may only contain local CLI files.
		{name: "local files only", files: []string{".platform/local/project.yaml"}, org: "fixed-org", stdin: "\n\n"},
		{name: "both config styles", files: []string{".upsun/config.yaml", ".platform.app.yaml"}, org: "fixed-org",
			stdin: "\n\n"},
	}
	t.Run("mismatch in non-interactive mode", func(t *testing.T) {
		f := f.forSubtest(t)
		f.dir = writeFiles(t, ".upsun/config.yaml")
		_, stderr, err := f.RunCombinedOutput(append(createArgs, "--org", "fixed-org", "--no-set-remote")...)
		require.NoError(t, err)
		assert.Contains(t, stderr, mismatch)
		assert.NotContains(t, stderr, "[y/N]")
	})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			f.dir = writeFiles(t, c.files...)
			before := len(apiHandler.Requests("POST", "/organizations/*/subscriptions"))
			_, stderr, err := f.RunInteractive(c.stdin, append(createArgs, "--org", c.org, "--no-set-remote")...)
			after := len(apiHandler.Requests("POST", "/organizations/*/subscriptions"))
			if c.wantMismatch {
				assert.Contains(t, stderr, mismatch)
			}
			if c.wantErr {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, "configuration, but the organization")
				assert.Equal(t, before, after)
				return
			}
			require.NoError(t, err)
			if !c.wantMismatch {
				assert.NotContains(t, stderr, "configuration, but the organization")
			}
		})
	}
}

func TestProjectCreateOrgType_CreateOrg(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	// The user has only a flexible organization.
	org := makeOrg("flex-org-id", "flex-org", "Flex Org", "my-user-id", "flexible")
	org.Links["create-subscription"] = mockapi.HALLink{HREF: "/organizations/flex-org-id/subscriptions"}
	apiHandler.SetOrgs([]*mockapi.Org{org})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	// Answers: create an organization, its label, name (default), country, and confirmation.
	// The mock API does not give the new organization a create-subscription link, so the command then fails.
	_, stderr, err := f.RunInteractive("y\nTest Org\n\nUS\ny\n",
		"project:create", "--title", "Test", "--region", "test-region", "--type", "fixed", "--no-set-remote")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "You do not yet own nor belong to a fixed organization in which you can create a project.")
	assert.Contains(t, stderr, "Created organization")
	requests := apiHandler.Requests("POST", "/organizations")
	require.Len(t, requests, 1)
	assert.Equal(t, "fixed", jsonObject(t, requests[0].Body)["type"])
}
