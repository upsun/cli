package lint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// upsunVendor is the first-party Upsun configuration used by most tests.
func upsunVendor() Vendor {
	return Vendor{Flavor: "upsun", ConfigDir: ".upsun"}
}

func TestDetectStyle(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		want    Style
		wantErr bool
	}{
		{"flex", map[string]string{".upsun/config.yaml": "applications: {}"}, StyleFlex, false},
		{"fixed via app file", map[string]string{".platform.app.yaml": "name: app\ntype: \"php:8.3\""}, StyleFixed, false},
		{"fixed via .platform dir", map[string]string{".platform/routes.yaml": "{}"}, StyleFixed, false},
		{"none", nil, StyleUnknown, true},
		// A per-app file buried in a subdirectory (e.g. a test fixture) must not,
		// on its own, mark the directory as a Fixed-style project.
		{"nested app file alone is not a project",
			map[string]string{"fixtures/example/.platform.app.yaml": "name: app"}, StyleUnknown, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for p, c := range tc.files {
				writeFile(t, filepath.Join(dir, p), c)
			}
			_, style, err := CheckDir(context.Background(), dir, upsunVendor())
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, style)
		})
	}
}

func TestCheckDir_Vendors(t *testing.T) {
	const app = "name: app\ntype: \"php:8.3\"\n"
	platformVendor := Vendor{Flavor: "platform", ConfigDir: ".platform", AppFile: ".platform.app.yaml"}
	acmeFixed := Vendor{Flavor: "platform", ConfigDir: ".acme", AppFile: "acme.app.yaml"}
	acmeFlex := Vendor{Flavor: "upsun", ConfigDir: ".acme"}

	cases := []struct {
		name    string
		vendor  Vendor
		files   map[string]string
		want    Style
		wantErr bool
	}{
		// Scenario B: Platform.sh CLI recognizes the Upsun Flex counterpart.
		{"platform: .upsun present wins", platformVendor,
			map[string]string{".upsun/config.yaml": "applications: {}", ".platform.app.yaml": app}, StyleFlex, false},
		{"platform: no .upsun is Fixed", platformVendor,
			map[string]string{".platform.app.yaml": app}, StyleFixed, false},
		// Scenario C: white-label Fixed uses its own names, no counterpart.
		{"white-label fixed via app file", acmeFixed,
			map[string]string{"acme.app.yaml": app}, StyleFixed, false},
		{"white-label fixed ignores .upsun counterpart", acmeFixed,
			map[string]string{".upsun/config.yaml": "applications: {}"}, StyleUnknown, true},
		// Scenario D: white-label Flex uses its own dir, no counterpart.
		{"white-label flex via its dir", acmeFlex,
			map[string]string{".acme/config.yaml": "applications: {}"}, StyleFlex, false},
		{"white-label flex ignores .platform counterpart", acmeFlex,
			map[string]string{".platform.app.yaml": app}, StyleUnknown, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for p, c := range tc.files {
				writeFile(t, filepath.Join(dir, p), c)
			}
			_, style, err := CheckDir(context.Background(), dir, tc.vendor)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, style)
		})
	}
}

func TestFindProjectRoot(t *testing.T) {
	t.Run("ascends to the enclosing git root", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main")
		sub := filepath.Join(root, "services", "api")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		assert.Equal(t, root, FindProjectRoot(sub))
	})

	t.Run("nearest git root wins over an outer one", func(t *testing.T) {
		outer := t.TempDir()
		writeFile(t, filepath.Join(outer, ".git", "HEAD"), "ref: refs/heads/main")
		inner := filepath.Join(outer, "vendor", "pkg")
		writeFile(t, filepath.Join(inner, ".git", "HEAD"), "ref: refs/heads/main")
		assert.Equal(t, inner, FindProjectRoot(inner))
	})
}

func TestLintFixed_StrayPlatformDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".platform.app.yaml"), "name: app\ntype: \"php:8.3\"")
	writeFile(t, filepath.Join(dir, "legacy", ".platform", "routes.yaml"), "{}")
	result, style, err := CheckDir(context.Background(), dir, upsunVendor())
	require.NoError(t, err)
	assert.Equal(t, StyleFixed, style)
	assert.Contains(t, result.String(), "legacy/.platform")
	assert.Contains(t, result.String(), "not at the project root and will be ignored")
}

func TestLintFixed_StrayUpsunDir(t *testing.T) {
	// A stray .upsun (the counterpart config dir) inside a Fixed project is also
	// ignored by the platform and should be warned about.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".platform.app.yaml"), "name: app\ntype: \"php:8.3\"")
	writeFile(t, filepath.Join(dir, "sub", ".upsun", "config.yaml"), "applications: {}")
	result, style, err := CheckDir(context.Background(), dir, upsunVendor())
	require.NoError(t, err)
	assert.Equal(t, StyleFixed, style)
	assert.Contains(t, result.String(), "sub/.upsun")
}

func TestLintDir_Flex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), `applications:
  app:
    type: "php:8.3"
    relationships:
      database: "db:postgresql"
services:
  db:
    type: "postgresql:16"
routes:
  "https://{default}/":
    type: upstream
    upstream: "app:http"
`)
	result, style, err := CheckDir(context.Background(), dir, upsunVendor())
	require.NoError(t, err)
	assert.Equal(t, StyleFlex, style)
	assert.False(t, result.HasErrors(), "expected no errors, got: %s", result)
}

func TestLintDir_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, style, err := CheckDir(context.Background(), dir, upsunVendor())
	require.Error(t, err)
	assert.Equal(t, StyleUnknown, style)
	assert.Contains(t, err.Error(), "no configuration found")
}

func TestLintDir_BothPresentPrefersFlex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), `applications:
  app:
    type: "php:8.3"
`)
	writeFile(t, filepath.Join(dir, ".platform.app.yaml"), "name: app")
	result, style, err := CheckDir(context.Background(), dir, upsunVendor())
	require.NoError(t, err)
	assert.Equal(t, StyleFlex, style)
	assert.True(t, result.HasWarnings())
	assert.Contains(t, result.String(), "both .upsun and .platform")
}
