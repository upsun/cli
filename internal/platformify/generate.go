package platformify

import (
	"bytes"
	"embed"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
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
	Relationships   map[string]relationship
}

type relationship struct {
	Service  string
	Endpoint string
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
		Relationships:   relationships(a.services),
	}
}

// relationships returns the relationship for each service, by service name.
func relationships(services []service) map[string]relationship {
	endpoints := map[string]string{
		"mariadb":          "mysql",
		"oracle-mysql":     "mysql",
		"chrome-headless":  "http",
		"redis-persistent": "redis",
	}
	r := make(map[string]relationship, len(services))
	for _, s := range services {
		endpoint, _, _ := strings.Cut(s.Type, ":")
		if e, ok := endpoints[endpoint]; ok {
			endpoint = e
		}
		r[s.Name] = relationship{Service: s.Name, Endpoint: endpoint}
	}
	return r
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
			files[strings.TrimPrefix(name, templatesDir+"/")] = content
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
