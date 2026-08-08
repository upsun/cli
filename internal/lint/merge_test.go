package lint

import (
	"testing"
	"testing/fstest"

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
	merged, err := mergeConfigFiles(fsys, []string{"a.yaml", "b.yml"})
	require.NoError(t, err)
	require.Contains(t, merged, "foo")
	require.Contains(t, merged, "db")
	require.Contains(t, merged, "/:")
}

func TestMergeConfigFiles_DuplicateKey(t *testing.T) {
	fsys := fstest.MapFS{
		"a.yaml": &fstest.MapFile{Data: []byte(`applications:
  foo: {type: go}`)},
		"b.yml": &fstest.MapFile{Data: []byte(`applications:
  foo: {type: node}`)},
	}
	_, err := mergeConfigFiles(fsys, []string{"a.yaml", "b.yml"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate key 'foo'")
}

func TestGetMergedConfigFiles_Success(t *testing.T) {
	fsys := fstest.MapFS{
		".upsun/a.yaml": &fstest.MapFile{Data: []byte(`applications:
  foo: {type: go}`)},
		".upsun/b.yml": &fstest.MapFile{Data: []byte(`services:
  db: {type: mariadb}`)},
	}
	merged, err := getMergedConfigFiles(fsys, ".", ".upsun")
	require.NoError(t, err)
	require.Contains(t, merged, "foo")
	require.Contains(t, merged, "db")
}

func TestGetMergedConfigFiles_NoUpsunDir(t *testing.T) {
	fsys := fstest.MapFS{}
	_, err := getMergedConfigFiles(fsys, ".", ".upsun")
	require.Error(t, err)
	require.Contains(t, err.Error(), ".upsun")
}

func TestGetMergedConfigFiles_NoYamlFiles(t *testing.T) {
	fsys := fstest.MapFS{
		".upsun/notyaml.txt": &fstest.MapFile{Data: []byte("not yaml")},
	}
	_, err := getMergedConfigFiles(fsys, ".", ".upsun")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no configuration files found")
}
