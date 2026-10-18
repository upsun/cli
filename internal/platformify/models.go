package platformify

import (
	"github.com/upsun/cli/pkg/registry"
)

// stack is a framework that the wizard detects and configures.
type stack int

const (
	genericStack stack = iota
	django
	laravel
	nextJS
	strapi
	flask
	express
	rails
)

// Name returns the stack's identifier, used in templates.
func (s stack) Name() string {
	return [...]string{"generic", "django", "laravel", "nextjs", "strapi", "flask", "express", "rails"}[s]
}

// Title returns the stack's display name.
func (s stack) Title() string {
	return [...]string{"Other", "Django", "Laravel", "Next.js", "Strapi", "Flask", "Express", "Rails"}[s]
}

// runtime returns the runtime type for the stack, or "" if it can be any.
func (s stack) runtime() string {
	switch s {
	case django, flask:
		return python
	case rails:
		return ruby
	case laravel:
		return php
	case nextJS, strapi, express:
		return nodejs
	default:
		return ""
	}
}

// Runtime types.
const (
	nodejs = "nodejs"
	php    = "php"
	python = "python"
	ruby   = "ruby"
)

// choice is an image type offered by the wizard, with its display title.
type choice struct {
	typ, title string
}

var runtimeChoices = []choice{
	{"dotnet", "C#/.Net Core"},
	{"elixir", "Elixir"},
	{"golang", "Go"},
	{"java", "Java"},
	{nodejs, "JavaScript/Node.js"},
	{php, "PHP"},
	{python, "Python"},
	{ruby, "Ruby"},
	{"rust", "Rust"},
}

var serviceChoices = []choice{
	{"mariadb", "MariaDB"},
	{"mysql", "MySQL"},
	{"postgresql", "PostgreSQL"},
	{"redis", "Redis"},
	{"redis-persistent", "Redis Persistent"},
	{"memcached", "Memcached"},
	{"opensearch", "OpenSearch"},
	{"solr", "Solr"},
	{"varnish", "Varnish"},
	{"kafka", "Kafka"},
	{"vault-kms", "Vault KMS"},
	{"rabbitmq", "RabbitMQ"},
	{"influxdb", "InfluxDB"},
	{"chrome-headless", "Chrome Headless"},
	{"network-storage", "Network Storage"},
	{"oracle-mysql", "Oracle MySQL"},
}

// ephemeralServices are service types that have no disk.
var ephemeralServices = []string{"chrome-headless", "memcached", "redis"}

// diskSizes are the disk sizes offered for services, in MB.
var diskSizes = []string{"1024", "2048", "3072", "4096", "5120"}

// titles returns the titles of the choices.
func titles(choices []choice) []string {
	t := make([]string, len(choices))
	for i, c := range choices {
		t[i] = c.title
	}
	return t
}

// typeByTitle returns the type of the choice with the given title.
func typeByTitle(choices []choice, title string) string {
	for _, c := range choices {
		if c.title == title {
			return c.typ
		}
	}
	return ""
}

// versions returns the supported versions of an image type, newest first.
func versions(typ string) []string {
	reg, err := registry.Load()
	if err != nil {
		return nil
	}
	return reg[typ].Versions.Supported
}

// Dependency managers.
const (
	pip      = "pip"
	poetry   = "poetry"
	pipenv   = "pipenv"
	composer = "composer"
	yarn     = "yarn"
	npm      = "npm"
	bundler  = "bundler"
)

var depManagerTitles = map[string]string{
	pip:      "Pip",
	poetry:   "Poetry",
	pipenv:   "Pipenv",
	composer: "Composer",
	yarn:     "Yarn",
	npm:      "Npm",
	bundler:  "Bundler",
}

// Socket families.
const (
	tcp  = "tcp"
	unix = "unix"
)

// answers holds what the wizard detects or asks.
type answers struct {
	stack              stack
	runtime            string // e.g. "python"
	version            string // e.g. "3.14"
	name               string
	applicationRoot    string
	environment        map[string]string
	buildSteps         []string
	webCommand         string
	socketFamily       string
	deployCommand      []string
	dependencyManagers []string
	dependencies       map[string]map[string]string
	buildFlavor        string
	disk               string
	mounts             map[string]map[string]string
	services           []service
	locations          map[string]map[string]any
}

// service is a service needed by the application.
type service struct {
	Name         string
	Type         string // e.g. "postgresql:18"
	TypeVersions []string
	Disk         string
	DiskSizes    []string
}

// pythonRunPrefix returns the command prefix for the Python dependency manager, if it uses a virtualenv.
func (a *answers) pythonRunPrefix() string {
	for _, dm := range a.dependencyManagers {
		switch dm {
		case pipenv:
			return "pipenv run "
		case poetry:
			return "poetry run "
		}
	}
	return ""
}
