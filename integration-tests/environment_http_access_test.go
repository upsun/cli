package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentHTTPAccess(t *testing.T) {
	t.Parallel()
	main := makeEnv(layoutProjectID, "main", "production", "active", nil)
	main.HTTPAccess = map[string]any{"is_enabled": true, "addresses": []any{}, "basic_auth": map[string]any{}}
	p := setUpProject(t, main)

	stdOut, stdErr, err := p.f.RunCombinedOutput("httpaccess", "-p", layoutProjectID, "-e", "main")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "HTTP access settings for the environment main:")
	assert.Contains(t, stdOut, "is_enabled: true")

	stdOut, stdErr, err = p.f.RunCombinedOutput("httpaccess", "-p", layoutProjectID, "-e", "main",
		"--auth", "user1:password1", "--access", "allow:192.0.2.1", "--access", "deny:any")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Updated HTTP access settings for the environment main:")
	assert.Contains(t, stdOut, "{ address: 192.0.2.1/32, permission: allow }")
	assert.Contains(t, stdOut, "user1: '******'")
	assert.NotContains(t, stdOut, "password1")
	assert.Equal(t, map[string]any{
		"is_enabled": true,
		"basic_auth": map[string]any{"user1": "password1"},
		"addresses": []any{
			map[string]any{"permission": "allow", "address": "192.0.2.1/32"},
			map[string]any{"permission": "deny", "address": "0.0.0.0/0"},
		},
	}, p.env("main").HTTPAccess)

	// Disable access control, and remove the credentials.
	stdOut, _, err = p.f.RunCombinedOutput("httpaccess", "-p", layoutProjectID, "-e", "main",
		"--enabled", "0", "--auth", "0")
	require.NoError(t, err)
	assert.Contains(t, stdOut, "is_enabled: false")
	httpAccess := p.env("main").HTTPAccess
	assert.Equal(t, false, httpAccess["is_enabled"])
	assert.Equal(t, map[string]any{}, httpAccess["basic_auth"])
	assert.Len(t, httpAccess["addresses"], 2)

	_, stdErr, err = p.f.RunCombinedOutput("httpaccess", "-p", layoutProjectID, "-e", "main", "--auth", "user1:short")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The minimum password length for --auth is 6 characters")

	_, stdErr, err = p.f.RunCombinedOutput("httpaccess", "-p", layoutProjectID, "-e", "main",
		"--access", "allow:example.com")
	assert.Error(t, err)
	assert.Contains(t, stdErr, `The address "example.com" is not a valid IP address or CIDR`)
}
