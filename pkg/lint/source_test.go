package lint

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLoadYAML_Tags(t *testing.T) {
	fsys := fstest.MapFS{
		".upsun/config.yaml": {Data: []byte(`applications:
  app: !include apps/app.yaml
  other: !include
    type: yaml
    path: apps/other.yaml
`)},
		".upsun/apps/app.yaml": {Data: []byte(`type: php:8.4
hooks:
  build: !include {type: string, path: ../scripts/build.sh}
  deploy: !include {type: binary, path: ../scripts/build.sh}
source:
  root: !archive ../../src
favicon: !file ../../src/favicon.ico
bundle: !include {type: archive, path: ../../src}
`)},
		".upsun/apps/other.yaml":  {Data: []byte("type: nodejs:24\n")},
		".upsun/scripts/build.sh": {Data: []byte("set -e\nmake\n")},
		"src/favicon.ico":         {Data: []byte("icon")},
		"src/index.php":           {Data: []byte("<?php")},
	}
	node, err := loadYAML(fsys, ".upsun/config.yaml")
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, node.Decode(&got))
	assert.Equal(t, map[string]any{"applications": map[string]any{
		"app": map[string]any{
			"type": "php:8.4",
			"hooks": map[string]any{
				"build":  "set -e\nmake\n",
				"deploy": "data:;base64,c2V0IC1lCm1ha2UK",
			},
			"source":  map[string]any{"root": "src"},
			"favicon": "src/favicon.ico",
			"bundle":  "src",
		},
		"other": map[string]any{"type": "nodejs:24"},
	}}, got)

	// Content from an included file is reported at the tag.
	assert.Equal(t, 2, lineOf(node, ".applications.app.hooks.build"))
	assert.Equal(t, 3, lineOf(node, ".applications.other.type"))
}

func TestLoadYAML_TagErrors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"missing file", "a: !include missing.yaml", "c.yaml:1: 'missing.yaml' doesn't exist in the repository"},
		{"outside the project", "a: !include ../../etc/passwd", "c.yaml:1: '../../etc/passwd' is outside the project"},
		{"absolute path", "a: !include /etc/passwd", "c.yaml:1: '/etc/passwd' is outside the project"},
		{"missing type", "a: !include {path: s.sh}", "c.yaml:1: the `!include` tag must have a `type` specified"},
		{"missing path", "a: !include {type: string}", "c.yaml:1: the `!include` tag must specify a `path`"},
		{"unknown type", "a: !include {type: text, path: s.sh}",
			"c.yaml:1: `!include` type must be one of archive, binary, string, yaml"},
		{"sequence value", "a: !include [s.sh]", "c.yaml:1: the `!include` tag value should be a scalar or a mapping"},
		{"file expected", "a: !file dir", "c.yaml:1: 'dir' doesn't point to a file"},
		{"directory expected", "a: !archive s.sh", "c.yaml:1: 's.sh' doesn't point to a directory"},
		{"archive of a file", "a: !include {type: archive, path: s.sh}", "c.yaml:1: `s.sh` doesn't point to a directory"},
		{"string of a directory", "\n\nb: !include {type: string, path: dir}", "c.yaml:3: `dir` doesn't point to a file"},
		{"recursive include", "a: !include loop.yaml", "loop.yaml:1: `c.yaml` is included recursively"},
		{"error in an included file", "a: !include bad.yaml", "bad.yaml:2: 'nope' doesn't exist in the repository"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"c.yaml":    {Data: []byte(c.content)},
				"s.sh":      {Data: []byte("echo")},
				"dir/f":     {Data: []byte("f")},
				"loop.yaml": {Data: []byte("b: !include c.yaml")},
				"bad.yaml":  {Data: []byte("ok: 1\nb: !file nope")},
			}
			_, err := loadYAML(fsys, "c.yaml")
			require.Error(t, err)
			assert.Equal(t, c.want, err.Error())
		})
	}
}

func TestLineOf(t *testing.T) {
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`applications:
  app:
    web:
      locations:
        "/":
          root: public
    authorizations:
      - type: env
        action: view
      - type: task
        action: operate
routes:
  "https://{default}/api":
    type: upstream
    upstream: "app:http"
`), &doc))
	root := doc.Content[0]
	cases := []struct {
		path string
		want int
	}{
		{".applications.app", 2},
		{`.applications.app.web.locations["/"].root`, 6},
		{".applications.app.web.locations./.root", 6},
		{".applications.app.authorizations.1.action", 11},
		{".applications.app.authorizations.1", 10},
		{".applications.app.missing.key", 2},
		{".routes.https://{default}/api.upstream", 15},
		{`.routes["https://{default}/api"].upstream`, 15},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			assert.Equal(t, c.want, lineOf(root, c.path))
		})
	}
}

func TestLoadYAML_DuplicateKeys(t *testing.T) {
	fsys := fstest.MapFS{"c.yaml": {Data: []byte("applications:\n  app: {}\napplications: null\n")}}
	_, err := loadYAML(fsys, "c.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `mapping key "applications" already defined`)
}

func TestMappingEntries_AliasesAndMergeKeys(t *testing.T) {
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`.base: &base
  type: php:8.4
  size: S
.apps: &apps
  app:
    <<: *base
    size: M
applications: *apps
`), &doc))
	root := doc.Content[0]
	apps := resolveAlias(mappingValue(root, "applications").node)
	require.NotNil(t, apps)
	app := mappingValue(apps, "app")
	require.NotNil(t, app)
	assert.Equal(t, 5, app.keyLine)

	values := map[string]string{}
	for _, e := range mappingEntries(resolveAlias(app.node)) {
		values[e.key.Value] = e.value.Value
	}
	// An explicit key overrides a merged one.
	assert.Equal(t, map[string]string{"type": "php:8.4", "size": "M"}, values)
	assert.Equal(t, 2, lineOf(root, ".applications.app.type"))
}
