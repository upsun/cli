package routeloops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		name         string
		routes       []Route
		wantCycles   []Cycle
		wantWarnings []Warning
	}{
		{
			name:   "empty",
			routes: nil,
		},
		{
			name: "single upstream, no cycles",
			routes: []Route{
				{URL: "https://{default}", Type: TypeUpstream},
			},
		},
		{
			name: "self-loop",
			routes: []Route{
				{URL: "https://{default}", Type: TypeRedirect, To: "https://{default}"},
			},
			wantCycles: []Cycle{
				{URLs: []string{"https://{default}", "https://{default}"}},
			},
		},
		{
			name: "two-node cycle",
			routes: []Route{
				{URL: "https://a.example.com", Type: TypeRedirect, To: "https://b.example.com"},
				{URL: "https://b.example.com", Type: TypeRedirect, To: "https://a.example.com"},
			},
			wantCycles: []Cycle{
				{URLs: []string{"https://a.example.com", "https://b.example.com", "https://a.example.com"}},
			},
		},
		{
			name: "three-node cycle deduped across starts",
			routes: []Route{
				{URL: "https://a", Type: TypeRedirect, To: "https://b"},
				{URL: "https://b", Type: TypeRedirect, To: "https://c"},
				{URL: "https://c", Type: TypeRedirect, To: "https://a"},
			},
			wantCycles: []Cycle{
				{URLs: []string{"https://a", "https://b", "https://c", "https://a"}},
			},
		},
		{
			name: "chain terminating in upstream: no cycle",
			routes: []Route{
				{URL: "http://{default}", Type: TypeRedirect, To: "https://{default}"},
				{URL: "https://{default}", Type: TypeUpstream},
			},
		},
		{
			name: "dangling redirect: no cycle, warning",
			routes: []Route{
				{URL: "https://a", Type: TypeRedirect, To: "https://nowhere"},
			},
			wantWarnings: []Warning{
				{URL: "https://a", Reason: "redirect target is not a known route: https://nowhere"},
			},
		},
		{
			name: "redirect with no `to`: warning, not cycle",
			routes: []Route{
				{URL: "https://a", Type: TypeRedirect, To: ""},
			},
			wantWarnings: []Warning{
				{URL: "https://a", Reason: "redirect route has no `to:` (uses `redirects.paths` only, or is malformed)"},
			},
		},
		{
			name: "cycle with lead-in tail: only cycle reported, tail dropped",
			routes: []Route{
				{URL: "https://lead", Type: TypeRedirect, To: "https://a"},
				{URL: "https://a", Type: TypeRedirect, To: "https://b"},
				{URL: "https://b", Type: TypeRedirect, To: "https://a"},
			},
			wantCycles: []Cycle{
				{URLs: []string{"https://a", "https://b", "https://a"}},
			},
		},
		{
			name: "trailing-slash and whitespace variants collapse to same cycle",
			routes: []Route{
				{URL: "https://a/", Type: TypeRedirect, To: " https://b "},
				{URL: "https://b", Type: TypeRedirect, To: "https://a"},
			},
			wantCycles: []Cycle{
				{URLs: []string{"https://a", "https://b", "https://a"}},
			},
		},
		{
			name: "smart-quoted values still detected",
			routes: []Route{
				{URL: "“https://{default}”", Type: TypeRedirect, To: "“http://{default}”"},
				{URL: "http://{default}", Type: TypeRedirect, To: "https://{default}"},
			},
			wantCycles: []Cycle{
				{URLs: []string{"http://{default}", "https://{default}", "http://{default}"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.routes)
			assert.Equal(t, tc.wantCycles, got.Cycles)
			assert.Equal(t, tc.wantWarnings, got.Warnings)
		})
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  https://example.com  ", "https://example.com"},
		{"“https://{default}/”", "https://{default}"},
		{"HTTPS://Example.COM/Path", "https://example.com/Path"},
		{"https://example.com/", "https://example.com"},
		{"https://example.com/path/", "https://example.com/path"},
		{"https://{default}", "https://{default}"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, normalize(tc.in))
		})
	}
}

func TestParseRoutesYAML(t *testing.T) {
	data := []byte(`
"https://{default}/":
    type: upstream
    upstream: app:http
"http://{default}":
    type: redirect
    to: "https://{default}/"
"https://ignored":
    type: something-else
`)
	routes, err := ParseRoutesYAML(data)
	require.NoError(t, err)
	require.Len(t, routes, 2)

	byURL := map[string]Route{}
	for _, r := range routes {
		byURL[r.URL] = r
	}
	assert.Equal(t, TypeUpstream, byURL["https://{default}/"].Type)
	assert.Equal(t, TypeRedirect, byURL["http://{default}"].Type)
	assert.Equal(t, "https://{default}/", byURL["http://{default}"].To)
}

func TestParseUpsunConfig(t *testing.T) {
	data := []byte(`
applications:
  app:
    type: php
routes:
  "https://{default}/":
    type: upstream
    upstream: app:http
  "http://{default}":
    type: redirect
    to: "https://{default}/"
`)
	routes, err := ParseUpsunConfig(data)
	require.NoError(t, err)
	assert.Len(t, routes, 2)
}

func TestParseUpsunConfig_NoRoutesSection(t *testing.T) {
	data := []byte(`applications:
  app:
    type: php
`)
	routes, err := ParseUpsunConfig(data)
	require.NoError(t, err)
	assert.Empty(t, routes)
}

func TestParseFile_AutoDetectsShape(t *testing.T) {
	dir := t.TempDir()

	platformFile := filepath.Join(dir, "routes.yaml")
	require.NoError(t, os.WriteFile(platformFile, []byte(`
"https://{default}/":
    type: upstream
    upstream: app:http
`), 0o644))

	upsunFile := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(upsunFile, []byte(`
routes:
  "https://{default}/":
    type: upstream
    upstream: app:http
`), 0o644))

	p, err := ParseFile(platformFile)
	require.NoError(t, err)
	require.Len(t, p, 1)
	assert.Equal(t, "https://{default}/", p[0].URL)

	u, err := ParseFile(upsunFile)
	require.NoError(t, err)
	require.Len(t, u, 1)
	assert.Equal(t, "https://{default}/", u[0].URL)
}

func TestParseLiveCSV(t *testing.T) {
	data := []byte(`"https://{default}/",upstream,app:http
"http://{default}",redirect,"https://{default}/"
`)
	routes, err := ParseLiveCSV(data)
	require.NoError(t, err)
	require.Len(t, routes, 2)

	assert.Equal(t, "https://{default}/", routes[0].URL)
	assert.Equal(t, TypeUpstream, routes[0].Type)
	assert.Equal(t, "", routes[0].To, "upstream rows should not populate To")

	assert.Equal(t, "http://{default}", routes[1].URL)
	assert.Equal(t, TypeRedirect, routes[1].Type)
	assert.Equal(t, "https://{default}/", routes[1].To)
}

func TestDiscoverProjectRoutes_PlatformFirst(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".platform"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".platform", "routes.yaml"),
		[]byte(`"https://{default}/": {type: upstream, upstream: "app:http"}`),
		0o644,
	))

	routes, src, err := DiscoverProjectRoutes(dir)
	require.NoError(t, err)
	assert.Len(t, routes, 1)
	assert.Contains(t, src, ".platform/routes.yaml")
}

func TestDiscoverProjectRoutes_UpsunMultiFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".upsun", "sub"), 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".upsun", "config.yaml"),
		[]byte(`
applications:
  app:
    type: php
routes:
  "https://a": {type: redirect, to: "https://b"}
`),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".upsun", "sub", "more-routes.yaml"),
		[]byte(`
routes:
  "https://b": {type: redirect, to: "https://a"}
`),
		0o644,
	))

	routes, src, err := DiscoverProjectRoutes(dir)
	require.NoError(t, err)
	assert.Len(t, routes, 2)
	assert.Contains(t, src, "config.yaml")
	assert.Contains(t, src, "more-routes.yaml")

	got := Detect(routes)
	require.Len(t, got.Cycles, 1, "loops across .upsun files should be caught")
}

func TestDiscoverProjectRoutes_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, _, err := DiscoverProjectRoutes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".platform/routes.yaml")
	assert.Contains(t, err.Error(), ".upsun")
}
