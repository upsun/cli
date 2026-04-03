package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionSwitch_WritesIDFile(t *testing.T) {
	// session:switch does not require API or auth server access.
	f := newCommandFactory(t, "", "")

	_, _, err := f.RunCombinedOutput("session:switch", "work")
	require.NoError(t, err)

	idFile := filepath.Join(f.homeDir, ".platform-test-cli", "session-id")
	data, err := os.ReadFile(idFile)
	require.NoError(t, err)
	assert.Equal(t, "work", strings.TrimSpace(string(data)))
}

func TestSessionSwitch_BlockedByEnvVar(t *testing.T) {
	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv, EnvPrefix+"SESSION_ID=env-session")

	_, stderr, err := f.RunCombinedOutput("session:switch", "other")
	assert.Error(t, err)
	assert.Contains(t, stderr, "environment variable")
}
