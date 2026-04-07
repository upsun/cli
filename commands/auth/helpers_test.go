package auth

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/legacy"
	"github.com/upsun/cli/internal/session"
)

// ---- formatValue ----

func TestFormatValue_Nil(t *testing.T) {
	assert.Equal(t, "", formatValue(nil))
}

func TestFormatValue_BoolTrue(t *testing.T) {
	assert.Equal(t, "true", formatValue(true))
}

func TestFormatValue_BoolFalse(t *testing.T) {
	assert.Equal(t, "false", formatValue(false))
}

func TestFormatValue_String(t *testing.T) {
	assert.Equal(t, "hello", formatValue("hello"))
}

func TestFormatValue_StringWithWhitespace(t *testing.T) {
	assert.Equal(t, "hello", formatValue("  hello  "))
}

func TestFormatValue_Number(t *testing.T) {
	assert.Equal(t, "42", formatValue(42))
}

// ---- printTable ----

func TestPrintTable_RendersHeaderAndRows(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"id":    "user-1",
		"email": "x@example.com",
	}
	printTable(&buf, []string{"id", "email"}, data)
	out := buf.String()

	assert.Contains(t, out, "Property")
	assert.Contains(t, out, "Value")
	assert.Contains(t, out, "id")
	assert.Contains(t, out, "user-1")
	assert.Contains(t, out, "email")
	assert.Contains(t, out, "x@example.com")
}

func TestPrintTable_ColumnsWideEnoughForContent(t *testing.T) {
	var buf bytes.Buffer
	longKey := "a_very_long_property_name"
	printTable(&buf, []string{longKey}, map[string]interface{}{longKey: "v"})
	out := buf.String()
	// Each data row must contain the key without truncation
	assert.Contains(t, out, longKey)
}

// ---- InjectSessionCredentials ----

func testCfg(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile("../../integration-tests/config.yaml")
	require.NoError(t, err)
	cfg, err := config.FromYAML(data)
	require.NoError(t, err)
	return cfg
}

func TestInjectSessionCredentials_EnvTokenAlreadySet(t *testing.T) {
	cfg := testCfg(t)
	t.Setenv(cfg.Application.EnvPrefix+"TOKEN", "env-token")

	wrapper := &legacy.CLIWrapper{}
	InjectSessionCredentials(cfg, wrapper)

	assert.Empty(t, wrapper.ExtraEnv, "should not inject when TOKEN env var is set")
}

func TestInjectSessionCredentials_InjectsAPITokenFromSession(t *testing.T) {
	cfg := testCfg(t)
	mgr, err := session.New(cfg)
	require.NoError(t, err)
	require.NoError(t, mgr.SetAPIToken("stored-api-token"))
	t.Cleanup(func() { _ = mgr.DeleteAPIToken() })

	wrapper := &legacy.CLIWrapper{}
	InjectSessionCredentials(cfg, wrapper)

	require.Len(t, wrapper.ExtraEnv, 1)
	assert.Equal(t, cfg.Application.EnvPrefix+"TOKEN=stored-api-token", wrapper.ExtraEnv[0])
}

func TestInjectSessionCredentials_InjectsOAuthAccessToken(t *testing.T) {
	cfg := testCfg(t)
	mgr, err := session.New(cfg)
	require.NoError(t, err)
	require.NoError(t, mgr.Save(&session.Session{
		AccessToken: "oauth-access-token",
		Expires:     time.Now().Add(time.Hour).Unix(),
	}))
	t.Cleanup(func() { _ = mgr.Delete() })

	wrapper := &legacy.CLIWrapper{}
	InjectSessionCredentials(cfg, wrapper)

	require.Len(t, wrapper.ExtraEnv, 1)
	assert.Equal(t, cfg.Application.EnvPrefix+"API_TOKEN=oauth-access-token", wrapper.ExtraEnv[0])
}

func TestInjectSessionCredentials_NoOpWhenNoCredentials(t *testing.T) {
	cfg := testCfg(t)

	wrapper := &legacy.CLIWrapper{}
	InjectSessionCredentials(cfg, wrapper)

	assert.Empty(t, wrapper.ExtraEnv)
}

// ---- printSessionID ----

func TestPrintSessionID_DefaultSingleSession_NoOutput(t *testing.T) {
	cfg := testCfg(t)
	mgr := session.NewWithStore(cfg, session.NewMemStore())

	var buf bytes.Buffer
	printSessionID(&buf, cfg, mgr)

	assert.Empty(t, buf.String())
}

func TestPrintSessionID_NonDefaultSession_PrintsHint(t *testing.T) {
	cfg := testCfg(t)
	// Use a non-default session ID
	cfg2 := *cfg
	cfg2.API.SessionID = "work"
	mgr := session.NewWithStore(&cfg2, session.NewMemStore())

	var buf bytes.Buffer
	printSessionID(&buf, &cfg2, mgr)

	out := buf.String()
	assert.Contains(t, out, "work")
	assert.Contains(t, out, "session:switch")
}

func TestPrintSessionID_MultipleSessionsShowsHint(t *testing.T) {
	// Load config first to get the env prefix, then set HOME before any dir is computed.
	base := testCfg(t)
	t.Setenv(base.Application.EnvPrefix+"HOME", t.TempDir())

	// Fresh config load after env var is set, so WritableUserDir cache is clean.
	cfg := testCfg(t)

	mgr, err := session.New(cfg)
	require.NoError(t, err)
	require.NoError(t, mgr.Save(&session.Session{AccessToken: "tok", Expires: time.Now().Add(time.Hour).Unix()}))

	mgr2 := session.NewWithID(cfg, "other")
	require.NoError(t, mgr2.Save(&session.Session{AccessToken: "tok2", Expires: time.Now().Add(time.Hour).Unix()}))

	var buf bytes.Buffer
	printSessionID(&buf, cfg, mgr)

	assert.Contains(t, buf.String(), "session ID")
}
