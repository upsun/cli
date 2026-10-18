package lint

import (
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

func TestDetectStyle(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		want    style
		wantErr bool
	}{
		{"flex", map[string]string{".upsun/config.yaml": "applications: {}"}, styleFlex, false},
		{"fixed via app file", map[string]string{".platform.app.yaml": "name: app\ntype: \"php:8.3\""}, styleFixed, false},
		{"fixed via .platform dir", map[string]string{".platform/routes.yaml": "{}"}, styleFixed, false},
		{"flex wins over fixed", map[string]string{
			".upsun/config.yaml": "applications: {}", ".platform.app.yaml": "name: app\ntype: \"php:8.3\""}, styleFlex, false},
		{"none", nil, styleUnknown, true},
		// A per-app file buried in a subdirectory (e.g. a test fixture) must not,
		// on its own, mark the directory as a Fixed-style project.
		{"nested app file alone is not a project",
			map[string]string{"fixtures/example/.platform.app.yaml": "name: app"}, styleUnknown, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for p, c := range tc.files {
				writeFile(t, filepath.Join(dir, p), c)
			}
			_, style, err := checkDir(dir)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, style)
		})
	}
}

func TestLintFixed_StrayPlatformDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".platform.app.yaml"), "name: app\ntype: \"php:8.3\"")
	writeFile(t, filepath.Join(dir, "legacy", ".platform", "routes.yaml"), "{}")
	result, style, err := checkDir(dir)
	require.NoError(t, err)
	assert.Equal(t, styleFixed, style)
	assert.Contains(t, result.String(), "legacy/.platform")
	assert.Contains(t, result.String(), "not at the project root and will be ignored")
}

func TestLintFixed_StrayUpsunDir(t *testing.T) {
	// A stray .upsun (the counterpart config dir) inside a Fixed project is also
	// ignored by the platform and should be warned about.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".platform.app.yaml"), "name: app\ntype: \"php:8.3\"")
	writeFile(t, filepath.Join(dir, "sub", ".upsun", "config.yaml"), "applications: {}")
	result, style, err := checkDir(dir)
	require.NoError(t, err)
	assert.Equal(t, styleFixed, style)
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
	result, style, err := checkDir(dir)
	require.NoError(t, err)
	assert.Equal(t, styleFlex, style)
	assert.False(t, result.HasErrors(), "expected no errors, got: %s", result)
}

func TestLintDir_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, style, err := checkDir(dir)
	require.Error(t, err)
	assert.Equal(t, styleUnknown, style)
	assert.Contains(t, err.Error(), "no configuration found")
}

func TestLintDir_BothPresentPrefersFlex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), `applications:
  app:
    type: "php:8.3"
`)
	writeFile(t, filepath.Join(dir, ".platform.app.yaml"), "name: app")
	result, style, err := checkDir(dir)
	require.NoError(t, err)
	assert.Equal(t, styleFlex, style)
	assert.True(t, result.HasWarnings())
	assert.Contains(t, result.String(), "both .upsun and .platform")
}

//nolint:lll
func TestLintDir_FlexSourceFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".upsun", "apps.yaml"), `applications:
  app:
    type: "php:8.4"
    authorizations:
      - type: task
        resource: missing
        action: operate
`)
	writeFile(t, filepath.Join(dir, ".upsun", "tasks.yaml"), `tasks:
  agent:
    type: "python:3.14"
    run:
      command: ./run
    extra: true
`)
	writeFile(t, filepath.Join(dir, ".upsun", "routes.yaml"), `routes:
  "https://{default}/api":
    type: upstream
    upstream: "missing:http"
`)
	result, _, err := checkDir(dir)
	require.NoError(t, err)
	assert.Equal(t, `Linter errors:
  - .upsun/tasks.yaml:2: tasks.agent: Additional property extra is not allowed`, result.String())

	// Semantic checks run once the schema passes.
	writeFile(t, filepath.Join(dir, ".upsun", "tasks.yaml"), `tasks:
  agent:
    type: "python:3.14"
    run:
      command: ./run
`)
	result, _, err = checkDir(dir)
	require.NoError(t, err)
	assert.Contains(t, result.String(), ".upsun/apps.yaml:6: applications.app.authorizations.0.resource: task 'missing' is not found")
	assert.Contains(t, result.String(), `.upsun/routes.yaml:4: routes["https://{default}/api"].upstream: upstream target 'missing' does not exist`)
}

func TestLintDir_SkipsSeparateCheckouts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), "applications:\n  app:\n    type: \"php:8.4\"\n")
	// A linked worktree, a separate clone, and a submodule, each with its own config.
	writeFile(t, filepath.Join(dir, ".claude", "worktrees", "wt", ".git"),
		"gitdir: "+filepath.Join(dir, ".git", "worktrees", "wt")+"\n")
	writeFile(t, filepath.Join(dir, ".claude", "worktrees", "wt", ".upsun", "config.yaml"), "applications: {}")
	writeFile(t, filepath.Join(dir, "clone", ".git", "HEAD"), "ref: refs/heads/main")
	writeFile(t, filepath.Join(dir, "clone", ".upsun", "config.yaml"), "applications: {}")
	writeFile(t, filepath.Join(dir, "sub", ".git"), "gitdir: ../.git/modules/sub\n")
	writeFile(t, filepath.Join(dir, "sub", ".upsun", "config.yaml"), "applications: {}")

	result, _, err := checkDir(dir)
	require.NoError(t, err)
	assert.Equal(t, []Issue{{
		File:    "sub/.upsun",
		Message: "this .upsun directory is not at the project root and will be ignored",
	}}, result.Warnings)
}

func TestLintFixed_WorktreeAppFileIgnored(t *testing.T) {
	dir := t.TempDir()
	app := "name: app\ntype: \"php:8.4\"\n"
	writeFile(t, filepath.Join(dir, ".platform.app.yaml"), app)
	// A worktree of the same repository would otherwise be read as a duplicate app.
	writeFile(t, filepath.Join(dir, "wt", ".git"), "gitdir: /elsewhere/.git/worktrees/wt\n")
	writeFile(t, filepath.Join(dir, "wt", ".platform.app.yaml"), app)

	result, style, err := checkDir(dir)
	require.NoError(t, err)
	assert.Equal(t, styleFixed, style)
	assert.False(t, result.HasErrors(), "unexpected errors: %s", result)
}

func TestLintDir_Includes(t *testing.T) {
	t.Run("flex", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), `applications:
  app: !include apps/app.yaml
  other: !include apps/missing.yaml
`)
		writeFile(t, filepath.Join(dir, ".upsun", "apps", "app.yaml"), `type: "php:8.4"
hooks:
  build: !include {type: string, path: ../build.sh}
`)
		writeFile(t, filepath.Join(dir, ".upsun", "build.sh"), "set -e\ncomposer install\n")

		result, _, err := checkDir(dir)
		require.NoError(t, err)
		assert.Equal(t, []Issue{{
			File: ".upsun/config.yaml", Line: 3, Message: "'apps/missing.yaml' doesn't exist in the repository",
		}}, result.Errors)

		writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), "applications:\n  app: !include apps/app.yaml\n")
		writeFile(t, filepath.Join(dir, ".upsun", "build.sh"), "set -e\ncomposer install (\n")
		result, _, err = checkDir(dir)
		require.NoError(t, err)
		require.Len(t, result.Errors, 1)
		assert.Equal(t, ".upsun/config.yaml", result.Errors[0].File)
		assert.Equal(t, 2, result.Errors[0].Line)
		assert.Equal(t, "applications.app.hooks.build", result.Errors[0].Path)
	})

	t.Run("fixed", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "search", ".platform.app.yaml"), `name: search
type: "python:3.14"
hooks:
  build: !include
    type: string
    path: build.sh
`)
		writeFile(t, filepath.Join(dir, "search", "build.sh"), "pip install -r requirements.txt\n")
		writeFile(t, filepath.Join(dir, ".platform", "routes.yaml"), `"https://{default}/":
  type: upstream
  upstream: "missing:http"
`)

		result, _, err := checkDir(dir)
		require.NoError(t, err)
		assert.Equal(t, []Issue{{
			File:    ".platform/routes.yaml",
			Line:    3,
			Path:    `routes["https://{default}/"].upstream`,
			Message: "upstream target 'missing' does not exist, available targets: search",
		}}, result.Errors)
	})
}

func TestLintDir_Aliases(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), `.apps: &apps
  app:
    type: "php:1.0"
applications: *apps
`)
	result, _, err := checkDir(dir)
	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, Issue{
		File: ".upsun/config.yaml", Line: 3, Path: "applications.app.type",
		Message: result.Errors[0].Message,
	}, result.Errors[0])
}

func TestLintDir_IncludeSymlinkOutsideProject(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "php:secret")
	dir := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link")))
	writeFile(t, filepath.Join(dir, ".upsun", "config.yaml"), `applications:
  app:
    type: !include {type: string, path: ../link/secret.txt}
`)
	result, _, err := checkDir(dir)
	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, Issue{
		File: ".upsun/config.yaml", Line: 3, Message: "'../link/secret.txt' is outside the project",
	}, result.Errors[0])
}
