package tests

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestOrgUserNoPermission covers the organization user commands when the user
// has no permission on the organization itself, as happens when they are only
// an admin on one of its projects. The API then returns an organization without
// the "members" and "invitations" links, and the commands must say so plainly
// instead of failing on an API error or an unresolved HAL link.
func TestOrgUserNoPermission(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"
	orgID := "org-id-no-perms"
	orgName := "no-perms-org"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})

	// An organization the user cannot administer: no "members" link, and no
	// "invitations" link. Only the owner-less "self" link is present.
	apiHandler.SetOrgs([]*mockapi.Org{makeOrg(orgID, orgName, "No Perms Org", "another-user-id", "flexible")})

	// The members endpoint is what the commands used to reach anyway, bypassing
	// the links: it must not be requested at all. The flag is atomic because the
	// handler runs on the test server's goroutine.
	var membersRequested atomic.Bool
	apiHandler.Get("/organizations/"+orgID+"/members", func(w http.ResponseWriter, _ *http.Request) {
		membersRequested.Store(true)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":403,"title":"Forbidden",` +
			`"detail":"You do not have access to view this organization's members."}`))
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	cases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "org:user:add",
			args:     []string{"org:user:add", "-o", orgName, "test@example.com"},
			expected: "You do not have permission to add users to the organization",
		},
		{
			name:     "org:user:update",
			args:     []string{"org:user:update", "-o", orgName, "test@example.com", "--permission", "billing"},
			expected: "You do not have permission to update users in the organization",
		},
		{
			name:     "org:user:delete",
			args:     []string{"org:user:delete", "-o", orgName, "test@example.com"},
			expected: "You do not have permission to delete users from the organization",
		},
		{
			// The read-only commands already behaved this way; keep them covered
			// so the wording of the whole family stays consistent.
			name:     "org:user:list",
			args:     []string{"org:user:list", "-o", orgName},
			expected: "You do not have permission to view users in the organization",
		},
		{
			name:     "org:user:get",
			args:     []string{"org:user:get", "-o", orgName, "test@example.com"},
			expected: "You do not have permission to view users in the organization",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := f.RunCombinedOutput(c.args...)
			require.Error(t, err, "the command must fail\nstdout: %s\nstderr: %s", stdout, stderr)
			assert.Contains(t, stderr, c.expected)
			assert.Contains(t, stderr, orgName)

			// None of the raw failures should surface.
			assert.NotContains(t, stderr, "Link not found")
			assert.NotContains(t, stderr, "ApiResponseException")
			assert.NotContains(t, stderr, "403 Forbidden")
			assert.NotContains(t, stderr, "Permission denied. Check your permissions")
			// Nor should the usage summary, which Symfony prints for exceptions.
			assert.NotContains(t, stderr, "[-o|--org ORG]")
		})
	}

	// Interactively, with no email given, org:user:update used to pick the user
	// from a members listing built from the organization URL, bypassing the links
	// entirely — which produced a raw 403 on top of the permission error.
	t.Run("org:user:update interactive", func(t *testing.T) {
		stdout, stderr, err := f.RunInteractive("", "org:user:update", "-o", orgName)
		require.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stderr, "You do not have permission to update users in the organization")
		assert.NotContains(t, stderr, "403 Forbidden")
		assert.NotContains(t, stderr, "ApiResponseException")
		assert.NotContains(t, stderr, "Permission denied. Check your permissions")
	})

	assert.False(t, membersRequested.Load(), "the members endpoint must not be requested without the link")
}

// TestOrgUserWithPermission is the counterpart: with the links present, the
// commands must get past the permission check and reach their normal behavior.
func TestOrgUserWithPermission(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"
	orgID := "org-id-with-perms"
	orgName := "with-perms-org"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})

	org := makeOrg(orgID, orgName, "With Perms Org", myUserID, "flexible")
	org.Links["members"] = mockapi.HALLink{HREF: "/organizations/" + url.PathEscape(orgID) + "/members"}
	org.Links["invitations"] = mockapi.HALLink{HREF: "/organizations/" + url.PathEscape(orgID) + "/invitations"}
	apiHandler.SetOrgs([]*mockapi.Org{org})

	// An empty members list: the commands get past the permission check and then
	// report that the user was not found.
	apiHandler.Get("/organizations/"+orgID+"/members", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"_links":{"self":{"href":"/organizations/` + orgID + `/members"}}}`))
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	cases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "org:user:update",
			args:     []string{"org:user:update", "-o", orgName, "test@example.com", "--permission", "billing"},
			expected: "was not found in the organization",
		},
		{
			name:     "org:user:delete",
			args:     []string{"org:user:delete", "-o", orgName, "test@example.com"},
			expected: "User not found",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := f.RunCombinedOutput(c.args...)
			require.Error(t, err, "stdout: %s\nstderr: %s", stdout, stderr)
			assert.Contains(t, stderr, c.expected)
			assert.NotContains(t, stderr, "You do not have permission")
		})
	}
}
