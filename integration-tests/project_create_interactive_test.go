package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestProjectCreate_AcceptDefaults sends two newlines to accept the
// "Default branch" prompt and the cost-confirmation [Y/n] prompt.
func TestProjectCreate_AcceptDefaults(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetOrgs([]*mockapi.Org{
		makeOrg("cli-test-id", "cli-tests", "CLI Test Org", "my-user-id", "flexible"),
	})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	stdOut, stdErr, err := f.RunInteractive(
		"\n\n",
		"project:create",
		"--title", "Interactive Defaults Test",
		"--region", "test-region",
		"--org", "cli-tests",
		"--no-set-remote",
	)
	require.NoError(t, err, "stdout: %s\nstderr: %s", stdOut, stdErr)

	assert.Contains(t, stdErr, "Default branch")
	assert.Contains(t, stdErr, "Are you sure you want to continue?")
	assert.Contains(t, stdErr, "[Y/n]")
	assert.NotEmpty(t, strings.TrimSpace(stdOut))
}
