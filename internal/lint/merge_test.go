package lint

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindFlexConfigFiles(t *testing.T) {
	fsys := fstest.MapFS{
		".upsun/a.yaml":      &fstest.MapFile{Data: []byte("{}")},
		".upsun/b.yaml":      &fstest.MapFile{Data: []byte("{}")},
		".upsun/c.yml":       &fstest.MapFile{Data: []byte("{}")},
		".upsun/notyaml.txt": &fstest.MapFile{Data: []byte("not yaml")},
	}
	files, err := findFlexConfigFiles(fsys, ".", ".upsun")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{".upsun/a.yaml", ".upsun/b.yaml", ".upsun/c.yml"}, files)
}

// io/fs glob patterns are always slash-separated, on every platform. A nested
// directory catches a pattern built with the OS separator, which would match
// nothing on Windows.
func TestFindFlexConfigFiles_NestedDir(t *testing.T) {
	fsys := fstest.MapFS{
		"sub/dir/.upsun/a.yaml": &fstest.MapFile{Data: []byte("{}")},
		"sub/dir/.upsun/b.yml":  &fstest.MapFile{Data: []byte("{}")},
	}
	files, err := findFlexConfigFiles(fsys, "sub/dir", ".upsun")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"sub/dir/.upsun/a.yaml", "sub/dir/.upsun/b.yml"}, files)

	_, err = findFlexConfigFiles(fstest.MapFS{}, "sub/dir", ".upsun")
	require.ErrorContains(t, err, "sub/dir/.upsun/*.yaml")
}

func TestMergeConfigFiles_Success(t *testing.T) {
	fsys := fstest.MapFS{
		"a.yaml": &fstest.MapFile{Data: []byte(`applications:
  foo: {type: go}
routes:
  /: {type: upstream}`)},
		"b.yml": &fstest.MapFile{Data: []byte(`services:
  db: {type: mariadb}`)},
	}
	merged, sources, err := mergeConfigFiles(fsys, []string{"a.yaml", "b.yml"})
	require.NoError(t, err)
	require.Contains(t, merged, "foo")
	require.Contains(t, merged, "db")
	require.Contains(t, merged, "/:")
	assert.Equal(t, map[string]string{
		"applications.foo": "a.yaml",
		"routes./":         "a.yaml",
		"services.db":      "b.yml",
	}, sources)
}

func TestMergeConfigFiles_Tasks(t *testing.T) {
	fsys := fstest.MapFS{
		"a.yaml": &fstest.MapFile{Data: []byte(`applications:
  foo: {type: go}`)},
		"b.yaml": &fstest.MapFile{Data: []byte(`tasks:
  agent: {type: "python:3.14", run: {command: ./run}}`)},
	}
	merged, sources, err := mergeConfigFiles(fsys, []string{"a.yaml", "b.yaml"})
	require.NoError(t, err)
	require.Contains(t, merged, "agent")
	assert.Equal(t, "b.yaml", sources["tasks.agent"])
}

func TestMergeConfigFiles_TopLevelKeys(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{"dot-prefixed key is ignored", ".anchors: {a: 1}\napplications:\n  foo: {type: go}", ""},
		{"unknown key", "applications:\n  foo: {type: go}\nworkers: {}", "unknown top-level key 'workers' in a.yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{"a.yaml": &fstest.MapFile{Data: []byte(c.content)}}
			merged, _, err := mergeConfigFiles(fsys, []string{"a.yaml"})
			if c.wantErr != "" {
				require.ErrorContains(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotContains(t, merged, "anchors")
		})
	}
}

//nolint:lll
func TestMergeConfigFiles_DuplicateKey(t *testing.T) {
	fsys := fstest.MapFS{
		"a.yaml": &fstest.MapFile{Data: []byte(`applications:
  foo: {type: go}`)},
		"b.yml": &fstest.MapFile{Data: []byte(`applications:
  foo: {type: node}`)},
	}
	_, _, err := mergeConfigFiles(fsys, []string{"a.yaml", "b.yml"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate key 'foo' in section 'applications' found in file b.yml (already defined in a.yaml)")
}

func TestGetMergedConfigFiles_Success(t *testing.T) {
	fsys := fstest.MapFS{
		".upsun/a.yaml": &fstest.MapFile{Data: []byte(`applications:
  foo: {type: go}`)},
		".upsun/b.yml": &fstest.MapFile{Data: []byte(`services:
  db: {type: mariadb}`)},
	}
	merged, _, err := getMergedConfigFiles(fsys, ".", ".upsun")
	require.NoError(t, err)
	require.Contains(t, merged, "foo")
	require.Contains(t, merged, "db")
}

func TestGetMergedConfigFiles_NoUpsunDir(t *testing.T) {
	fsys := fstest.MapFS{}
	_, _, err := getMergedConfigFiles(fsys, ".", ".upsun")
	require.Error(t, err)
	require.Contains(t, err.Error(), ".upsun")
}

func TestGetMergedConfigFiles_NoYamlFiles(t *testing.T) {
	fsys := fstest.MapFS{
		".upsun/notyaml.txt": &fstest.MapFile{Data: []byte("not yaml")},
	}
	_, _, err := getMergedConfigFiles(fsys, ".", ".upsun")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no configuration files found")
}
