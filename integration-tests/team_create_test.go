package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestTeamCreateNoLabel is a regression test for a TypeError raised by the
// legacy team:create command when --label was omitted in non-interactive mode.
func TestTeamCreateNoLabel(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "user-for-team-create-test"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	org := makeOrg("org-id-1", "acme", "ACME Inc.", myUserID, "flexible")
	org.Capabilities = []string{"teams"}
	org.Links = mockapi.MakeHALLinks(
		"self=/organizations/org-id-1",
		"members=/organizations/org-id-1/members",
	)
	apiHandler.SetOrgs([]*mockapi.Org{org})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stderr, err := f.RunCombinedOutput("team:create", "--org", "acme", "--no-check-unique")
	require.Error(t, err)
	assert.Contains(t, stderr, "The --label option is required in non-interactive mode.")
	assert.NotContains(t, stderr, "TypeError")
}
