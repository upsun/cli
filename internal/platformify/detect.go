package platformify

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	settingsPyFile = "settings.py"
	managePyFile   = "manage.py"
	composerJSON   = "composer.json"
	packageJSON    = "package.json"
)

// detectStack sets the stack from the project's files.
func (w *wizard) detectStack() error {
	w.answers.stack = w.findStack()
	if w.answers.stack != genericStack {
		w.detected("stack", w.answers.stack.Title())
	}
	if w.answers.stack == genericStack && w.isSymfony() {
		return w.suggestSymfonyCLI()
	}
	return nil
}

func (w *wizard) findStack() stack {
	dir := w.dir
	if fileExists(dir, settingsPyFile) && fileExists(dir, managePyFile) {
		return django
	}
	if p := findFile(dir, "config.ru"); p != "" && fileContains(p, "Rails.application.load_server") {
		return rails
	}
	if p := findFile(dir, "requirements.txt"); p != "" && fileContains(p, "flask") {
		return flask
	}
	if p := findFile(dir, "pyproject.toml"); p != "" {
		if _, ok := tomlValue(p, "tool", "poetry", "dependencies", "flask"); ok {
			return flask
		}
	}
	if p := findFile(dir, "Pipfile"); p != "" {
		if _, ok := tomlValue(p, "packages", "flask"); ok {
			return flask
		}
	}
	for _, p := range findAllFiles(dir, composerJSON) {
		if _, ok := jsonValue(p, "require", "laravel/framework"); ok {
			return laravel
		}
	}
	for _, p := range findAllFiles(dir, packageJSON) {
		if _, ok := jsonValue(p, "dependencies", "next"); ok {
			return nextJS
		}
		if _, ok := jsonValue(p, "dependencies", "@strapi/strapi"); ok {
			return strapi
		}
		if _, ok := jsonValue(p, "dependencies", "strapi"); ok {
			return strapi
		}
		if _, ok := jsonValue(p, "dependencies", "express"); ok {
			return express
		}
	}
	return genericStack
}

// isSymfony reports whether the project uses Symfony full-stack, and not Ibexa or Shopware.
func (w *wizard) isSymfony() bool {
	isSymfony := fileExists(w.dir, "symfony.lock")
	for _, p := range findAllFiles(w.dir, composerJSON) {
		if _, ok := jsonValue(p, "autoload", "psr-0", "shopware"); ok {
			return false
		}
		if _, ok := jsonValue(p, "autoload", "psr-4", `shopware\core\`); ok {
			return false
		}
		if _, ok := jsonValue(p, "autoload", "psr-4", `shopware\appbundle\`); ok {
			return false
		}
		if keywords, ok := jsonValue(p, "keywords"); ok {
			if k, ok := keywords.([]any); ok && slices.ContainsFunc(k, func(v any) bool {
				s, _ := v.(string)
				return strings.EqualFold(s, "shopware")
			}) {
				return false
			}
		}
		if require, ok := jsonValue(p, "require"); ok {
			if r, ok := require.(map[string]any); ok {
				if _, ok := r["symfony/framework-bundle"]; ok {
					isSymfony = true
				}
				for name := range r {
					if strings.HasPrefix(name, "shopware/") || strings.HasPrefix(name, "ibexa/") ||
						strings.HasPrefix(name, "ezsystems/") {
						return false
					}
				}
			}
		}
	}
	return isSymfony
}

// detectDependencyManagers sets the dependency managers from lock files.
func (w *wizard) detectDependencyManagers() error {
	a := w.answers
	a.dependencies = map[string]map[string]string{}
	a.buildFlavor = "none"

	switch {
	case fileExists(w.dir, "poetry.lock"):
		a.dependencyManagers = append(a.dependencyManagers, poetry)
	case fileExists(w.dir, "Pipfile.lock"):
		a.dependencyManagers = append(a.dependencyManagers, pipenv)
	case fileExists(w.dir, "requirements.txt"):
		a.dependencyManagers = append(a.dependencyManagers, pip)
	}
	if fileExists(w.dir, "composer.lock") {
		a.dependencyManagers = append(a.dependencyManagers, composer)
		a.dependencies[php] = map[string]string{"composer/composer": "^2"}
	}
	switch {
	case fileExists(w.dir, "yarn.lock"):
		a.dependencyManagers = append(a.dependencyManagers, yarn)
		a.dependencies[nodejs] = map[string]string{"yarn": "^1.22.0"}
	case fileExists(w.dir, "package-lock.json"):
		a.dependencyManagers = append(a.dependencyManagers, npm)
	}
	if fileExists(w.dir, "Gemfile.lock") {
		a.dependencyManagers = append(a.dependencyManagers, bundler)
	}

	if len(a.dependencyManagers) > 0 {
		names := make([]string, len(a.dependencyManagers))
		for i, dm := range a.dependencyManagers {
			names[i] = depManagerTitles[dm]
		}
		w.detected("dependency managers", strings.Join(names, ", "))
	}
	return nil
}

// detectApplicationRoot sets the application root to the directory of the first dependency manager's file.
func (w *wizard) detectApplicationRoot() error {
	files := map[string]string{
		composer: composerJSON,
		npm:      packageJSON,
		yarn:     packageJSON,
		poetry:   "pyproject.toml",
		pipenv:   "Pipfile",
		pip:      "requirements.txt",
	}
	for _, dm := range w.answers.dependencyManagers {
		name, ok := files[dm]
		if !ok {
			continue
		}
		if p := findFile(w.dir, name); p != "" {
			w.answers.applicationRoot, _ = filepath.Rel(w.dir, filepath.Dir(p))
			return nil
		}
	}
	return nil
}

// appDir returns the absolute path of the application root.
func (w *wizard) appDir() string {
	return filepath.Join(w.dir, w.answers.applicationRoot)
}

// setEnvironment sets environment variables for the dependency managers.
func (w *wizard) setEnvironment() error {
	a := w.answers
	a.environment = map[string]string{}
	for _, dm := range a.dependencyManagers {
		switch dm {
		case poetry:
			a.environment["POETRY_VERSION"] = "1.8.4"
			a.environment["POETRY_VIRTUALENVS_IN_PROJECT"] = "true"
		case pipenv:
			a.environment["PIPENV_TOOL_VERSION"] = "2024.2.0"
			a.environment["PIPENV_VENV_IN_PROJECT"] = "1"
		}
	}
	return nil
}

// setBuildSteps sets the build hook commands.
func (w *wizard) setBuildSteps() error {
	a := w.answers
	for _, dm := range a.dependencyManagers {
		switch dm {
		case poetry:
			a.buildSteps = append(a.buildSteps,
				"# Set PIP_USER to 0 so that Poetry does not complain",
				"export PIP_USER=0",
				"# Install poetry as a global tool",
				"python -m venv /app/.global",
				"pip install poetry==$POETRY_VERSION",
				"poetry install",
			)
		case pipenv:
			a.buildSteps = append(a.buildSteps,
				"# Set PIP_USER to 0 so that Pipenv does not complain",
				"export PIP_USER=0",
				"# Install Pipenv as a global tool",
				"python -m venv /app/.global",
				"pip install pipenv==$PIPENV_TOOL_VERSION",
				"pipenv sync",
			)
		case pip:
			a.buildSteps = append(a.buildSteps, "pip install -r requirements.txt")
		case yarn, npm:
			if a.runtime != nodejs {
				if _, ok := a.dependencies[nodejs]; !ok {
					a.dependencies[nodejs] = map[string]string{}
				}
				a.dependencies[nodejs]["n"] = "*"
				a.dependencies[nodejs]["npx"] = "*"
				a.environment["N_PREFIX"] = "/app/.global"
				a.buildSteps = append(a.buildSteps, "n auto || n lts", "hash -r")
			}
			_, hasBuild := jsonValue(filepath.Join(w.dir, packageJSON), "scripts", "build")
			if dm == yarn {
				a.buildSteps = append(a.buildSteps, "yarn")
				if hasBuild {
					a.buildSteps = append(a.buildSteps, "yarn build")
				}
			} else {
				a.buildSteps = append(a.buildSteps, "npm i")
				if hasBuild {
					a.buildSteps = append(a.buildSteps, "npm run build")
				}
			}
		case composer:
			a.buildSteps = append(a.buildSteps,
				"composer --no-ansi --no-interaction install --no-progress --prefer-dist --optimize-autoloader --no-dev",
			)
		case bundler:
			a.buildSteps = append(a.buildSteps, "bundle install")
		}
	}

	switch a.stack {
	case django:
		if managePy := w.managePyPath(); managePy != "" {
			a.buildSteps = append(a.buildSteps,
				"# Collect static files so that they can be served by "+w.assets.ServiceName,
				fmt.Sprintf("%spython %s collectstatic --noinput", a.pythonRunPrefix(), managePy),
			)
		}
	case nextJS:
		// Fall back to "next build" if there is no build script.
		if !slices.Contains(a.buildSteps, "yarn build") && !slices.Contains(a.buildSteps, "npm run build") {
			if slices.Contains(a.dependencyManagers, yarn) {
				a.buildSteps = append(a.buildSteps, "yarn exec next build")
			} else {
				a.buildSteps = append(a.buildSteps, "npm exec next build")
			}
		}
	case rails:
		a.environment["RAILS_ENV"] = "production"
		a.environment["PIDFILE"] = "tmp/server.pid"
		a.buildSteps = append(a.buildSteps, "bundle exec rails assets:precompile")
	}
	return nil
}

// managePyPath returns the path of Django's manage.py relative to the application root, or "".
func (w *wizard) managePyPath() string {
	p := findFile(w.appDir(), managePyFile)
	if p == "" {
		return ""
	}
	rel, _ := filepath.Rel(w.appDir(), p)
	return filepath.ToSlash(rel)
}

// setDeployCommand sets the deploy hook commands.
func (w *wizard) setDeployCommand() error {
	a := w.answers
	switch a.stack {
	case django:
		if managePy := w.managePyPath(); managePy != "" {
			a.deployCommand = append(a.deployCommand, fmt.Sprintf("%spython %s migrate", a.pythonRunPrefix(), managePy))
		}
	case laravel:
		a.deployCommand = append(a.deployCommand,
			"mkdir -p storage/framework/sessions",
			"mkdir -p storage/framework/cache",
			"mkdir -p storage/framework/views",
			"php artisan migrate --force",
			"php artisan optimize:clear",
		)
	case rails:
		a.deployCommand = append(a.deployCommand, "bundle exec rake db:migrate")
	}
	return nil
}

// setSocketFamily sets how the web server listens: on a Unix socket or a TCP port.
func (w *wizard) setSocketFamily() error {
	switch w.answers.runtime {
	case php:
	case ruby, python:
		w.answers.socketFamily = unix
	default:
		w.answers.socketFamily = tcp
	}
	return nil
}

// setWebCommand sets the command to start the web server.
func (w *wizard) setWebCommand() error {
	a := w.answers
	if a.runtime == php {
		return nil
	}
	listen := `listen to the "$SOCKET" Unix socket`
	if a.socketFamily == tcp {
		listen = `listen on the "$PORT" port`
	}
	a.webCommand = fmt.Sprintf(
		"echo 'Put your web server command in here! You need to %s. Read more about it here: %s#web-commands'; sleep 60",
		listen, w.assets.Docs().AppReference,
	)

	packageJSONPath := filepath.Join(w.dir, packageJSON)
	npmStart := func() string {
		if slices.Contains(a.dependencyManagers, yarn) {
			return "NODE_ENV=production yarn start"
		}
		return "NODE_ENV=production npm start"
	}
	switch a.stack {
	case django:
		pythonPath := ""
		wsgi := "app.wsgi"
		if wsgiPath := findFile(w.appDir(), "wsgi.py"); wsgiPath != "" {
			wsgiDir := filepath.Dir(wsgiPath)
			wsgi = filepath.Base(wsgiDir) + ".wsgi"
			// Add the Python path if the WSGI module's package is not directly in the application root.
			if rel, _ := filepath.Rel(w.appDir(), filepath.Dir(wsgiDir)); rel != "." {
				pythonPath = "--pythonpath=" + filepath.Base(filepath.Dir(wsgiDir))
			}
		}
		bind := "unix:$SOCKET"
		if a.socketFamily == tcp {
			bind = "0.0.0.0:$PORT"
		}
		a.webCommand = fmt.Sprintf("%sgunicorn %s -b %s %s --log-file -", a.pythonRunPrefix(), pythonPath, bind, wsgi)
	case rails:
		if a.socketFamily == tcp {
			a.webCommand = "bundle exec rails server"
		} else {
			a.webCommand = "bundle exec puma -b unix://$SOCKET"
		}
	case nextJS:
		a.webCommand = "npx next start -p $PORT"
	case strapi:
		if _, ok := jsonValue(packageJSONPath, "scripts", "start"); ok {
			a.webCommand = npmStart()
		}
	case express:
		if _, ok := jsonValue(packageJSONPath, "scripts", "start"); ok {
			a.webCommand = npmStart()
		} else if main, ok := jsonValue(packageJSONPath, "main"); ok {
			if s, ok := main.(string); ok {
				a.webCommand = "node " + s
			}
		} else if index := findFile(w.dir, "index.js"); index != "" {
			rel, _ := filepath.Rel(w.dir, index)
			a.webCommand = "node " + filepath.ToSlash(rel)
		}
	case flask:
		for _, name := range []string{"app.py", "server.py", "api.py"} {
			if _, err := os.Stat(filepath.Join(w.dir, name)); err != nil {
				continue
			}
			app := fmt.Sprintf("'%s:app'", strings.TrimSuffix(name, ".py"))
			bind := "unix:$SOCKET"
			if a.socketFamily == tcp {
				bind = "0.0.0.0:$PORT"
			}
			a.webCommand = fmt.Sprintf("%sgunicorn -b %s %s --log-file -", a.pythonRunPrefix(), bind, app)
			break
		}
	}
	return nil
}

// setLocations sets the web locations.
func (w *wizard) setLocations() error {
	a := w.answers
	a.locations = map[string]map[string]any{}
	switch {
	case a.stack == django:
		a.locations["/static"] = map[string]any{
			"root":    "static",
			"expires": "1h",
			"allow":   true,
		}
	case a.runtime == php:
		location := map[string]any{ //nolint:gosec // Not a credential.
			"passthru": "/index.php",
			"root":     "",
		}
		if index := findFile(w.dir, "index.php"); index != "" {
			if rel, _ := filepath.Rel(w.dir, filepath.Dir(index)); rel != "." {
				location["root"] = filepath.ToSlash(rel)
			}
		}
		a.locations["/"] = location
	}
	return nil
}

// setMounts sets the disk and mounts for the stack.
func (w *wizard) setMounts() error {
	a := w.answers
	local := func(sourcePath string) map[string]string {
		return map[string]string{"source": "local", "source_path": sourcePath}
	}
	switch a.stack {
	case rails:
		a.disk = "2048"
		tmp := map[string]string{"source": "tmp", "source_path": "tmp"}
		a.mounts = map[string]map[string]string{
			"storage": local("storage"),
			"tmp":     tmp,
			"log":     tmp,
		}
	case laravel:
		a.disk = "2048"
		a.mounts = map[string]map[string]string{
			"storage":         local("storage"),
			"bootstrap/cache": local("cache"),
			"/.config":        local("config"),
		}
	case nextJS:
		a.disk = "512"
		a.mounts = map[string]map[string]string{
			"/.npm": local("npm"),
		}
	case strapi:
		a.disk = "1024"
		a.mounts = map[string]map[string]string{
			"/.cache":        local("cache"),
			"/.tmp":          local("app"),
			"database":       local("database"),
			"extensions":     local("extensions"),
			"public/uploads": local("uploads"),
		}
	}
	return nil
}
