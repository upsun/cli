package platformify

import (
	"bytes"
	"embed"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"github.com/upsun/cli/internal/config"
)

//go:embed all:templates
var templatesFS embed.FS

// importSettingsLine imports the generated settings_psh.py into Django's settings.py.
const importSettingsLine = "from .settings_psh import *"

func newAssets(cnf *config.Config) *assets {
	return &assets{
		Binary:      cnf.Application.Executable,
		EnvPrefix:   strings.TrimSuffix(cnf.Service.EnvPrefix, "_"),
		ServiceName: cnf.Service.Name,
		docsBaseURL: cnf.Service.DocsURL,
		flex:        cnf.Service.ProjectConfigFlavor == "upsun",
	}
}

// templateInput is the data passed to templates.
type templateInput struct {
	Assets          *assets
	Stack           stack
	Name            string
	Type            string // e.g. "python:3.14"
	Runtime         string // e.g. "python"
	ApplicationRoot string
	Environment     map[string]string
	BuildSteps      []string
	WebCommand      string
	SocketFamily    string
	DeployCommand   []string
	Locations       map[string]map[string]any
	Dependencies    map[string]map[string]string
	BuildFlavor     string
	Disk            string
	Mounts          map[string]map[string]string
	Services        []service
}

func (w *wizard) templateInput() *templateInput {
	a := w.answers
	typ := a.runtime
	if a.version != "" {
		typ += ":" + a.version
	}
	locations := map[string]map[string]any{"/": {"passthru": true}}
	maps.Copy(locations, a.locations)
	return &templateInput{
		Assets:          w.assets,
		Stack:           a.stack,
		Name:            a.name,
		Type:            typ,
		Runtime:         a.runtime,
		ApplicationRoot: path.Join("/", filepath.ToSlash(a.applicationRoot)),
		Environment:     a.environment,
		BuildSteps:      a.buildSteps,
		WebCommand:      a.webCommand,
		SocketFamily:    a.socketFamily,
		DeployCommand:   a.deployCommand,
		Locations:       locations,
		Dependencies:    a.dependencies,
		BuildFlavor:     a.buildFlavor,
		Disk:            a.disk,
		Mounts:          a.mounts,
		Services:        a.services,
	}
}

// ServiceNames returns the sorted names of the services.
func (t *templateInput) ServiceNames() []string {
	names := make([]string, len(t.Services))
	for i, s := range t.Services {
		names[i] = s.Name
	}
	slices.Sort(names)
	return names
}

// Database returns the name of the first database service.
func (t *templateInput) Database() string {
	return t.firstService("mariadb", "mysql", "oracle-mysql", "postgresql")
}

// DatabaseUpper returns Database as an environment variable name.
func (t *templateInput) DatabaseUpper() string {
	return envName(t.Database())
}

// Cache returns the name of the first cache service.
func (t *templateInput) Cache() string {
	return t.firstService("redis", "redis-persistent", "memcached")
}

// CacheUpper returns Cache as an environment variable name.
func (t *templateInput) CacheUpper() string {
	return envName(t.Cache())
}

func (t *templateInput) firstService(types ...string) string {
	for _, s := range t.Services {
		for _, typ := range types {
			if strings.Contains(s.Type, typ) {
				return s.Name
			}
		}
	}
	return ""
}

func envName(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
}

// generate renders the configuration files for the project in dir.
// It returns their contents by slash-separated path relative to dir.
func generate(dir string, input *templateInput) (map[string][]byte, error) {
	templatesDir := "templates/fixed"
	if input.Assets.flex {
		templatesDir = "templates/flex"
	}
	files := map[string][]byte{}
	err := fs.WalkDir(templatesFS, templatesDir, func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := render(name, input)
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(content)) > 0 {
			files[outputPath(strings.TrimPrefix(name, templatesDir+"/"), input.ApplicationRoot)] = content
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if input.Stack == django {
		if err := generateDjango(dir, input, files); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// appFiles are written in the application root, and the other files in the project root.
var appFiles = []string{".environment", ".platform.app.yaml"}

// outputPath returns the path for a template's output, relative to the project root.
func outputPath(name, appRoot string) string {
	if slices.Contains(appFiles, name) {
		return strings.TrimPrefix(path.Join(appRoot, name), "/")
	}
	return name
}

// generateDjango adds a settings_psh.py file next to settings.py, and imports it there.
func generateDjango(dir string, input *templateInput, files map[string][]byte) error {
	settingsPath := findFile(filepath.Join(dir, filepath.FromSlash(input.ApplicationRoot)), settingsPyFile)
	if settingsPath == "" {
		return nil
	}
	rel, err := filepath.Rel(dir, filepath.Dir(settingsPath))
	if err != nil {
		return err
	}
	content, err := render("templates/django/settings_psh.py", input)
	if err != nil {
		return err
	}
	files[path.Join(filepath.ToSlash(rel), "settings_psh.py")] = content

	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		return err
	}
	if !bytes.Contains(settings, []byte(importSettingsLine)) {
		settingsRel, _ := filepath.Rel(dir, settingsPath)
		files[filepath.ToSlash(settingsRel)] = append(settings, []byte("\n\n"+importSettingsLine+"\n")...)
	}
	return nil
}

func render(name string, input *templateInput) ([]byte, error) {
	tpl, err := template.New(path.Base(name)).Funcs(sprig.FuncMap()).ParseFS(templatesFS, name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, input); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
