package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Auth holds the authentication settings.
//
// They are read from the same sources and with the same precedence as the legacy CLI: the embedded config, then the
// user's config.yaml file, then environment variables.
type Auth struct {
	Token       string // An API token (api.token).
	TokenFile   string // A file containing an API token (api.token_file), resolved to an absolute path.
	AccessToken string // A raw access token (api.access_token), never stored or refreshed.

	SessionID        string // The session ID (api.session_id), defaulting to "default".
	SessionIDFromEnv bool   // Whether the session ID was set via the <PREFIX>SESSION_ID environment variable.

	BaseURL      string // The API base URL (api.base_url).
	AuthURL      string // The auth server URL (api.auth_url).
	AuthorizeURL string // The OAuth 2.0 authorization endpoint (api.oauth2_auth_url).
	TokenURL     string // The OAuth 2.0 token endpoint (api.oauth2_token_url).
	RevokeURL    string // The OAuth 2.0 revocation endpoint (api.oauth2_revoke_url).
	ClientID     string // The OAuth 2.0 client ID (api.oauth2_client_id).

	DisableCredentialHelpers bool // Whether to store credentials in files instead of the keychain.
	SkipSSL                  bool // Whether to skip TLS verification (api.skip_ssl).
	DisableLocks             bool // Whether to skip locking (api.disable_locks).
}

// authKey describes an auth config key under "api", and the environment variables (without the prefix) that set it.
type authKey struct {
	key     string
	envVars []string // In ascending order of precedence.
	target  func(a *authSources) *string
}

// authSources holds the raw string values, as they are overridden by each source.
type authSources struct {
	token, tokenFile, accessToken, sessionID                      string
	baseURL, authURL, authorizeURL, tokenURL, revokeURL, clientID string
	disableCredentialHelpers, skipSSL, disableLocks               string
}

// authKeys lists the keys that are read, with their env var names.
//
// The generic form API_<KEY> comes first, then the legacy CLI's aliases, which take precedence. In the legacy CLI
// "API_TOKEN" is an alias for api.access_token (deprecated), overriding the generic form for api.token.
var authKeys = []authKey{
	{"token", []string{"TOKEN"}, func(a *authSources) *string { return &a.token }},
	{"token_file", []string{"API_TOKEN_FILE"}, func(a *authSources) *string { return &a.tokenFile }},
	{"access_token", []string{"API_ACCESS_TOKEN", "API_TOKEN"}, func(a *authSources) *string { return &a.accessToken }},
	{"session_id", []string{"API_SESSION_ID"}, func(a *authSources) *string { return &a.sessionID }},
	{"base_url", []string{"API_BASE_URL", "API_URL"}, func(a *authSources) *string { return &a.baseURL }},
	{"auth_url", []string{"API_AUTH_URL", "AUTH_URL"}, func(a *authSources) *string { return &a.authURL }},
	{"oauth2_auth_url", []string{"API_OAUTH2_AUTH_URL", "OAUTH2_AUTH_URL"},
		func(a *authSources) *string { return &a.authorizeURL }},
	{"oauth2_token_url", []string{"API_OAUTH2_TOKEN_URL", "OAUTH2_TOKEN_URL"},
		func(a *authSources) *string { return &a.tokenURL }},
	{"oauth2_revoke_url", []string{"API_OAUTH2_REVOKE_URL", "OAUTH2_REVOKE_URL"},
		func(a *authSources) *string { return &a.revokeURL }},
	{"oauth2_client_id", []string{"API_OAUTH2_CLIENT_ID", "OAUTH2_CLIENT_ID"},
		func(a *authSources) *string { return &a.clientID }},
	{"disable_credential_helpers", []string{"API_DISABLE_CREDENTIAL_HELPERS"},
		func(a *authSources) *string { return &a.disableCredentialHelpers }},
	{"skip_ssl", []string{"API_SKIP_SSL", "SKIP_SSL"}, func(a *authSources) *string { return &a.skipSSL }},
	{"disable_locks", []string{"API_DISABLE_LOCKS", "DISABLE_LOCKS"},
		func(a *authSources) *string { return &a.disableLocks }},
}

// GoAuthEnabled reports whether authentication is handled in Go, instead of by the legacy CLI.
//
// It is set by the <PREFIX>GO_AUTH environment variable, which the legacy CLI also reads.
func (c *Config) GoAuthEnabled() bool {
	return phpBool(os.Getenv(c.Application.EnvPrefix + "GO_AUTH"))
}

var sessionIDPattern = regexp.MustCompile(`(?i)^[a-z0-9_-]+$`)

// ValidateSessionID checks a user-provided session ID.
func ValidateSessionID(id string) error {
	if strings.HasPrefix(id, "api-token-") || !sessionIDPattern.MatchString(id) {
		return fmt.Errorf("invalid session ID: %s", id)
	}
	return nil
}

// Auth reads the authentication settings.
func (c *Config) Auth() (*Auth, error) {
	src := &authSources{
		token:        c.API.Token,
		tokenFile:    c.API.TokenFile,
		accessToken:  c.API.AccessToken,
		baseURL:      c.API.BaseURL,
		authURL:      c.API.AuthURL,
		authorizeURL: c.API.OAuth2AuthorizeURL,
		tokenURL:     c.API.OAuth2TokenURL,
		revokeURL:    c.API.OAuth2RevokeURL,
		clientID:     c.API.OAuth2ClientID,
		sessionID:    c.API.SessionID,
	}
	if c.API.DisableCredentialHelpers {
		src.disableCredentialHelpers = "1"
	}
	if c.API.SkipSSL {
		src.skipSSL = "1"
	}
	if c.API.DisableLocks {
		src.disableLocks = "1"
	}

	userConfigDir, err := c.UserConfigDir()
	if err != nil {
		return nil, err
	}
	if err := applyUserAuthConfig(src, filepath.Join(userConfigDir, "config.yaml")); err != nil {
		return nil, err
	}

	prefix := c.Application.EnvPrefix
	for _, k := range authKeys {
		for _, v := range k.envVars {
			if val, ok := os.LookupEnv(prefix + v); ok {
				*k.target(src) = val
			}
		}
	}

	a := &Auth{
		Token:                    src.token,
		AccessToken:              src.accessToken,
		SessionID:                src.sessionID,
		BaseURL:                  src.baseURL,
		AuthURL:                  src.authURL,
		AuthorizeURL:             src.authorizeURL,
		TokenURL:                 src.tokenURL,
		RevokeURL:                src.revokeURL,
		ClientID:                 src.clientID,
		DisableCredentialHelpers: phpBool(src.disableCredentialHelpers),
		SkipSSL:                  phpBool(src.skipSSL),
		DisableLocks:             phpBool(src.disableLocks),
	}

	if src.tokenFile != "" {
		a.TokenFile = src.tokenFile
		if !filepath.IsAbs(a.TokenFile) && !strings.HasPrefix(a.TokenFile, `\`) {
			a.TokenFile = filepath.Join(userConfigDir, a.TokenFile)
		}
	}

	// The session ID file is only read if <PREFIX>SESSION_ID is not set.
	if envID, ok := os.LookupEnv(prefix + "SESSION_ID"); ok {
		a.SessionID = envID
	} else {
		fileID, err := c.readSessionIDFile()
		if err != nil {
			return nil, err
		}
		if fileID != "" {
			a.SessionID = fileID
		}
	}
	if a.SessionID == "" {
		a.SessionID = "default"
	}
	if err := ValidateSessionID(a.SessionID); err != nil {
		return nil, err
	}
	a.SessionIDFromEnv = a.SessionID != "default" && a.SessionID == os.Getenv(prefix+"SESSION_ID")

	if a.AuthURL != "" {
		base := strings.TrimRight(a.AuthURL, "/")
		for _, d := range []struct {
			target *string
			path   string
		}{
			{&a.AuthorizeURL, "/oauth2/authorize"},
			{&a.TokenURL, "/oauth2/token"},
			{&a.RevokeURL, "/oauth2/revoke"},
		} {
			if *d.target == "" {
				*d.target = base + d.path
			}
		}
	}
	if a.ClientID == "" {
		a.ClientID = c.Application.Slug
	}

	return a, nil
}

// SessionIDFile returns the path to the file where the session ID is saved by session:switch.
func (c *Config) SessionIDFile() (string, error) {
	dir, err := c.WritableUserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session-id"), nil
}

func (c *Config) readSessionIDFile() (string, error) {
	path, err := c.SessionIDFile()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if err := ValidateSessionID(id); err != nil {
		return "", fmt.Errorf("invalid session ID in file: %s", path)
	}
	return id, nil
}

// UserConfigDir returns the absolute path to the user config directory, e.g. ~/.upsun-cli.
func (c *Config) UserConfigDir() (string, error) {
	home, err := c.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, c.Application.UserConfigDir), nil
}

// applyUserAuthConfig reads the "api" keys from the user's config file, if it exists.
func applyUserAuthConfig(src *authSources, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var userConfig struct {
		API map[string]any `yaml:"api"`
	}
	if err := yaml.Unmarshal(b, &userConfig); err != nil {
		return fmt.Errorf("invalid config file %s: %w", path, err)
	}
	for _, k := range authKeys {
		v, ok := userConfig.API[k.key]
		if !ok || v == nil {
			continue
		}
		switch v := v.(type) {
		case bool:
			if v {
				*k.target(src) = "1"
			} else {
				*k.target(src) = ""
			}
		default:
			*k.target(src) = fmt.Sprint(v)
		}
	}
	return nil
}

// phpBool converts a string to a boolean in the same way as PHP.
func phpBool(s string) bool {
	return s != "" && s != "0"
}
