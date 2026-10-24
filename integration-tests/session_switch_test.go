package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestSessionSwitch_WritesIDFile(t *testing.T) {
	t.Parallel()
	// session:switch does not require API or auth server access.
	f := newCommandFactory(t, "", "")

	_, _, err := f.RunCombinedOutput("session:switch", "work")
	require.NoError(t, err)

	idFile := filepath.Join(f.home, ".platform-test-cli", "session-id")
	data, err := os.ReadFile(idFile)
	require.NoError(t, err)
	assert.Equal(t, "work", strings.TrimSpace(string(data)))
}

func TestSessionSwitch_BlockedByEnvVar(t *testing.T) {
	t.Parallel()
	f := newCommandFactory(t, "", "")
	f.extraEnv = append(f.extraEnv, EnvPrefix+"SESSION_ID=env-session")

	_, stderr, err := f.RunCombinedOutput("session:switch", "other")
	assert.Error(t, err)
	assert.Contains(t, stderr, "environment variable")
}

// TestSessionSwitch_InvalidID checks that an invalid session ID is refused, and not saved.
func TestSessionSwitch_InvalidID(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", "api-token-abc", "a/b", "a b"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			f := newCommandFactory(t, "", "")

			_, stderr, err := f.RunCombinedOutput("session:switch", id)
			assertExitCode(t, 1, err)
			assert.Contains(t, stderr, "Invalid session ID")
			assert.NoFileExists(t, filepath.Join(f.home, ".platform-test-cli", "session-id"))
		})
	}
}

// TestSessionSwitch_Default checks that switching to the default session deletes the ID file.
func TestSessionSwitch_Default(t *testing.T) {
	t.Parallel()
	f := newCommandFactory(t, "", "")
	idFile := filepath.Join(f.home, ".platform-test-cli", "session-id")

	_, _, err := f.RunCombinedOutput("session:switch", "work")
	require.NoError(t, err)
	require.FileExists(t, idFile)

	_, stderr, err := f.RunCombinedOutput("session:switch", "default")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Session ID changed from work to default")
	assert.NoFileExists(t, idFile)
}

// TestSessionSwitch_Prompt checks that the prompt suggests the only other session, without the sessions for API tokens.
func TestSessionSwitch_Prompt(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	writeOAuthSession(t, f.home, "work", map[string]any{
		"accessToken": "work-token", "expires": time.Now().Add(time.Hour).Unix(),
	})
	// Save tokens for the API token set via the environment.
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=")

	_, stderr, err := f.RunInteractive("\n", "session:switch")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "Enter a new session ID (default: work)")
	assert.Contains(t, stderr, "Session ID changed from default to work")
	assert.Equal(t, "work-token", f.Run("auth:token", "--no-warn"))
}

// TestSessionSwitch_StoredAPIToken checks that an API token saved by auth:api-token-login is only used in its session.
func TestSessionSwitch_StoredAPIToken(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = append(f.extraEnv, EnvPrefix+"TOKEN=", EnvPrefix+"API_WRITE_USER_SSH_CONFIG=0")
	writeOAuthSession(t, f.home, "default", map[string]any{
		"accessToken": "default-token", "expires": time.Now().Add(time.Hour).Unix(),
	})

	_, stderr, err := f.RunCombinedOutput("session:switch", "ci")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "To log in, run:")
	_, stderr, err = f.RunInteractive(mockapi.ValidAPITokens[1]+"\n", "auth:api-token-login")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, "access-token-2", f.Run("auth:token", "--no-warn"))

	_, stderr, err = f.RunCombinedOutput("session:switch", "default")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "Username: testuser")
	assert.Equal(t, "default-token", f.Run("auth:token", "--no-warn"))

	_, _, err = f.RunCombinedOutput("session:switch", "ci")
	require.NoError(t, err)
	assert.Equal(t, "access-token-2", f.Run("auth:token", "--no-warn"))
}

// TestSessionSwitch_EnvToken checks that an API token set via the environment is used in every session.
func TestSessionSwitch_EnvToken(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := newMyUserAPI(t)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	writeOAuthSession(t, f.home, "work", map[string]any{
		"accessToken": "work-token", "expires": time.Now().Add(time.Hour).Unix(),
	})

	_, stderr, err := f.RunCombinedOutput("session:switch", "work")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, "access-token-1", f.Run("auth:token", "--no-warn"))
}
