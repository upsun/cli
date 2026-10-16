package tests

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitConsent(t *testing.T) {
	t.Parallel()
	f := newCommandFactory(t, "", "")
	f.dir = t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", f.dir).Run())

	_, stdErr, err := f.RunCombinedOutput("init", "--ai=false")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "configuration without AI is no longer available")

	_, stdErr, err = f.RunCombinedOutput("init")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "use --yes to confirm in non-interactive mode")

	// Without --no-interaction, consent is not assumed when stdin is not a terminal.
	cmd := f.buildCommand("init")
	cmd.Env = slices.DeleteFunc(cmd.Env, func(e string) bool { return strings.HasPrefix(e, EnvPrefix+"NO_INTERACTION=") })
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	f.logStderr(string(out))
	assertExitCode(t, 1, err)
	assert.Contains(t, string(out), "use --yes to confirm in non-interactive mode")

	_, stdErr, err = f.RunInteractive("n\n", "init")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "It sends a sanitized repository digest to OpenAI")
	assert.Contains(t, stdErr, "Do you want to continue?")
	assert.NotContains(t, stdErr, "Authentication is required")
}
