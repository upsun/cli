package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/config"
)

func TestAuth(t *testing.T) {
	cases := []struct {
		name       string
		env        map[string]string
		userConfig string
		sessionID  string // The content of the session-id file.
		check      func(t *testing.T, a *config.Auth)
		wantErr    string
	}{
		{
			name: "defaults",
			check: func(t *testing.T, a *config.Auth) {
				assert.Equal(t, "default", a.SessionID)
				assert.False(t, a.SessionIDFromEnv)
				assert.Equal(t, "https://api.example.com", a.BaseURL)
				assert.Equal(t, "https://auth.example.com/oauth2/authorize", a.AuthorizeURL)
				assert.Equal(t, "https://auth.example.com/oauth2/token", a.TokenURL)
				assert.Equal(t, "https://auth.example.com/oauth2/revoke", a.RevokeURL)
				assert.Equal(t, "example-cli", a.ClientID)
				assert.Empty(t, a.Token)
				assert.False(t, a.DisableCredentialHelpers)
			},
		},
		{
			name: "env aliases",
			env: map[string]string{
				"TOKEN":         "api-token",
				"API_TOKEN":     "access-token",
				"AUTH_URL":      "https://auth2.example.com/",
				"API_URL":       "https://api2.example.com",
				"API_BASE_URL":  "https://ignored.example.com",
				"SESSION_ID":    "foo",
				"SKIP_SSL":      "1",
				"DISABLE_LOCKS": "true",
			},
			check: func(t *testing.T, a *config.Auth) {
				assert.Equal(t, "api-token", a.Token)
				assert.Equal(t, "access-token", a.AccessToken)
				assert.Equal(t, "https://auth2.example.com/oauth2/token", a.TokenURL)
				assert.Equal(t, "https://api2.example.com", a.BaseURL)
				assert.Equal(t, "foo", a.SessionID)
				assert.True(t, a.SessionIDFromEnv)
				assert.True(t, a.SkipSSL)
				assert.True(t, a.DisableLocks)
			},
		},
		{
			name: "env generic forms",
			env: map[string]string{ //nolint:gosec // test values
				"API_TOKEN_FILE":                 "token.txt",
				"API_ACCESS_TOKEN":               "access-token",
				"API_AUTH_URL":                   "https://auth3.example.com",
				"API_OAUTH2_TOKEN_URL":           "https://token.example.com",
				"API_OAUTH2_CLIENT_ID":           "my-client",
				"API_SESSION_ID":                 "bar",
				"API_DISABLE_CREDENTIAL_HELPERS": "1",
				"API_SKIP_SSL":                   "0",
			},
			check: func(t *testing.T, a *config.Auth) {
				assert.True(t, filepath.IsAbs(a.TokenFile))
				assert.Equal(t, "token.txt", filepath.Base(a.TokenFile))
				assert.Equal(t, "access-token", a.AccessToken)
				assert.Equal(t, "https://auth3.example.com/oauth2/authorize", a.AuthorizeURL)
				assert.Equal(t, "https://token.example.com", a.TokenURL)
				assert.Equal(t, "my-client", a.ClientID)
				assert.Equal(t, "bar", a.SessionID)
				assert.False(t, a.SessionIDFromEnv)
				assert.True(t, a.DisableCredentialHelpers)
				assert.False(t, a.SkipSSL)
			},
		},
		{
			name: "empty env var overrides config",
			env:  map[string]string{"TOKEN": ""},
			userConfig: `api:
  token: from-file
`,
			check: func(t *testing.T, a *config.Auth) {
				assert.Empty(t, a.Token)
			},
		},
		{
			name: "user config file",
			userConfig: `api:
  token: from-file
  token_file: /abs/token
  auth_url: https://auth4.example.com
  disable_credential_helpers: true
  session_id: baz
`,
			check: func(t *testing.T, a *config.Auth) {
				assert.Equal(t, "from-file", a.Token)
				assert.Equal(t, "/abs/token", a.TokenFile)
				assert.Equal(t, "https://auth4.example.com/oauth2/revoke", a.RevokeURL)
				assert.True(t, a.DisableCredentialHelpers)
				assert.Equal(t, "baz", a.SessionID)
			},
		},
		{
			name:       "env overrides user config",
			env:        map[string]string{"TOKEN": "from-env"},
			userConfig: "api: {token: from-file}\n",
			check: func(t *testing.T, a *config.Auth) {
				assert.Equal(t, "from-env", a.Token)
			},
		},
		{
			name:      "session ID file",
			sessionID: "from-file\n",
			env:       map[string]string{"API_SESSION_ID": "generic"},
			check: func(t *testing.T, a *config.Auth) {
				assert.Equal(t, "from-file", a.SessionID)
			},
		},
		{
			name:      "session ID env overrides file",
			sessionID: "from-file",
			env:       map[string]string{"SESSION_ID": "from-env"},
			check: func(t *testing.T, a *config.Auth) {
				assert.Equal(t, "from-env", a.SessionID)
			},
		},
		{
			name:    "invalid session ID",
			env:     map[string]string{"SESSION_ID": "a/b"},
			wantErr: "invalid session ID: a/b",
		},
		{
			name:    "reserved session ID",
			env:     map[string]string{"SESSION_ID": "api-token-abc"},
			wantErr: "invalid session ID",
		},
		{
			name:      "invalid session ID file",
			sessionID: "a b",
			wantErr:   "invalid session ID in file",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cnf, err := config.FromYAML([]byte(validConfig))
			require.NoError(t, err)
			home := t.TempDir()
			t.Setenv("EXAMPLE_CLI_HOME", home)
			for k, v := range c.env {
				t.Setenv("EXAMPLE_CLI_"+k, v)
			}
			dir := filepath.Join(home, ".example-cli")
			require.NoError(t, os.MkdirAll(dir, 0o700))
			if c.userConfig != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(c.userConfig), 0o600))
			}
			if c.sessionID != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "session-id"), []byte(c.sessionID), 0o600))
			}

			a, err := cnf.Auth()
			if c.wantErr != "" {
				assert.ErrorContains(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			c.check(t, a)
		})
	}
}
