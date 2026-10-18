package platformify

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTree creates files in a temporary directory and returns its path.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	return dir
}

func TestFindStack(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  stack
	}{
		{"empty", nil, genericStack},
		{"django", map[string]string{"manage.py": "", "mysite/settings.py": ""}, django},
		{"rails", map[string]string{"config.ru": "Rails.application.load_server"}, rails},
		{"flask requirements", map[string]string{"requirements.txt": "Flask==3.0"}, flask},
		{"flask poetry", map[string]string{"pyproject.toml": "[tool.poetry.dependencies]\nFlask = \"^3\"\n"}, flask},
		{"laravel", map[string]string{"composer.json": `{"require": {"laravel/framework": "^11"}}`}, laravel},
		{"nextjs", map[string]string{"package.json": `{"dependencies": {"next": "^15"}}`}, nextJS},
		{"strapi", map[string]string{"package.json": `{"dependencies": {"@strapi/strapi": "^5"}}`}, strapi},
		{"express", map[string]string{"package.json": `{"dependencies": {"express": "^5"}}`}, express},
		{"node_modules ignored", map[string]string{
			"node_modules/x/package.json": `{"dependencies": {"next": "1"}}`,
		}, genericStack},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &wizard{dir: writeTree(t, c.files)}
			assert.Equal(t, c.want, w.findStack())
		})
	}
}

func TestIsSymfony(t *testing.T) {
	cases := []struct {
		name     string
		composer string
		want     bool
	}{
		{"symfony", `{"require": {"symfony/framework-bundle": "^7"}}`, true},
		{"not symfony", `{"require": {"monolog/monolog": "^3"}}`, false},
		{"ibexa", `{"require": {"symfony/framework-bundle": "^7", "ibexa/core": "^4"}}`, false},
		{"shopware keyword", `{"keywords": ["Shopware"], "require": {"symfony/framework-bundle": "^7"}}`, false},
		{"shopware autoload", `{"autoload": {"psr-4": {"Shopware\\Core\\": "src/"}},
			"require": {"symfony/framework-bundle": "^7"}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &wizard{dir: writeTree(t, map[string]string{"composer.json": c.composer})}
			assert.Equal(t, c.want, w.isSymfony())
		})
	}
}

func TestSetBuildSteps(t *testing.T) {
	cases := []struct {
		name    string
		answers answers
		want    []string
	}{
		{
			name:    "Next.js with Yarn",
			answers: answers{stack: nextJS, runtime: nodejs, dependencyManagers: []string{yarn}},
			want:    []string{"yarn", "yarn exec next build"},
		},
		{
			name:    "Next.js with npm",
			answers: answers{stack: nextJS, runtime: nodejs, dependencyManagers: []string{npm}},
			want:    []string{"npm i", "npm exec next build"},
		},
		{
			name:    "Bundler",
			answers: answers{runtime: ruby, dependencyManagers: []string{bundler}},
			want:    []string{"bundle install"},
		},
		{
			name:    "Rails",
			answers: answers{stack: rails, runtime: ruby, dependencyManagers: []string{bundler}},
			want:    []string{"bundle install", "bundle exec rails assets:precompile"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.answers.environment = map[string]string{}
			c.answers.dependencies = map[string]map[string]string{}
			w := &wizard{dir: t.TempDir(), assets: &assets{}, answers: &c.answers}
			require.NoError(t, w.setBuildSteps())
			assert.Equal(t, c.want, c.answers.buildSteps)
		})
	}
}

func TestSetWebCommandExpressMain(t *testing.T) {
	dir := writeTree(t, map[string]string{"package.json": `{"main": "src/Server.js"}`})
	w := &wizard{dir: dir, assets: &assets{}, answers: &answers{stack: express, runtime: nodejs, socketFamily: tcp}}
	require.NoError(t, w.setWebCommand())
	assert.Equal(t, "node src/Server.js", w.answers.webCommand)
}

func TestSubdirectoryApp(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		check func(t *testing.T, a *answers)
	}{
		{
			name: "laravel",
			files: map[string]string{
				"app/composer.json":    `{"require": {"laravel/framework": "^11"}}`,
				"app/composer.lock":    "{}",
				"app/public/index.php": "",
			},
			check: func(t *testing.T, a *answers) {
				assert.Equal(t, "app", a.applicationRoot)
				assert.Equal(t, "public", a.locations["/"]["root"])
			},
		},
		{
			name: "express",
			files: map[string]string{
				"frontend/package.json":      `{"dependencies": {"express": "^5"}, "scripts": {"build": "tsc"}}`,
				"frontend/package-lock.json": "{}",
				"frontend/src/index.js":      "",
			},
			check: func(t *testing.T, a *answers) {
				assert.Equal(t, "frontend", a.applicationRoot)
				assert.Equal(t, []string{"npm i", "npm run build"}, a.buildSteps)
				assert.Equal(t, "node src/index.js", a.webCommand)
			},
		},
		{
			name: "flask",
			files: map[string]string{
				"api/requirements.txt": "flask\n",
				"api/app.py":           "",
			},
			check: func(t *testing.T, a *answers) {
				assert.Equal(t, "api", a.applicationRoot)
				assert.Equal(t, "gunicorn -b unix:$SOCKET 'app:app' --log-file -", a.webCommand)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &wizard{dir: writeTree(t, c.files), stdout: io.Discard, stderr: io.Discard, assets: &assets{},
				answers: &answers{}}
			require.NoError(t, w.detectStack())
			w.answers.runtime = w.answers.stack.runtime()
			for _, step := range w.detectionSteps() {
				require.NoError(t, step())
			}
			c.check(t, w.answers)
		})
	}
}
