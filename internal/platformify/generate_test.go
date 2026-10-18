package platformify

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/lint"
)

func TestGenerate(t *testing.T) {
	for _, flex := range []bool{true, false} {
		a := &assets{Binary: "upsun", EnvPrefix: "PLATFORM", ServiceName: "Upsun", docsBaseURL: "https://docs.upsun.com",
			flex: flex}
		for name, in := range testInputs() {
			t.Run(name, func(t *testing.T) {
				in.Assets = a
				dir := t.TempDir()
				files, err := generate(dir, in)
				require.NoError(t, err)

				for f := range files {
					assert.Contains(t, a.configFiles(), f)
				}
				require.NoError(t, writeFiles(dir, files))
				result, err := lint.CheckDir(dir)
				require.NoError(t, err)
				assert.False(t, result.HasErrors(), result.String())
			})
		}
	}
}

func TestGenerateDjango(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "app", "mysite", "settings.py")
	require.NoError(t, os.MkdirAll(filepath.Dir(settings), 0o755))
	require.NoError(t, os.WriteFile(settings, []byte("DEBUG = False\n"), 0o600))

	in := testInputs()["django"]
	in.Assets = &assets{flex: true}
	in.ApplicationRoot = "/app"
	files, err := generate(dir, in)
	require.NoError(t, err)
	assert.Contains(t, files, "app/mysite/settings_psh.py")
	assert.Equal(t, "DEBUG = False\n\n\n"+importSettingsLine+"\n", string(files["app/mysite/settings.py"]))

	// The import is not added twice.
	require.NoError(t, writeFiles(dir, files))
	files, err = generate(dir, in)
	require.NoError(t, err)
	assert.Contains(t, files, "app/mysite/settings_psh.py")
	assert.NotContains(t, files, "app/mysite/settings.py")
}

func TestRelationships(t *testing.T) {
	services := []service{
		{Name: "mysql", Type: "mysql:5.7"},
		{Name: "mariadb", Type: "mariadb:11.8"},
		{Name: "oracle-mysql", Type: "oracle-mysql:8.4"},
		{Name: "chrome-headless", Type: "chrome-headless:120"},
		{Name: "redis-persistent", Type: "redis-persistent:8.0"},
	}
	assert.Equal(t, map[string]relationship{
		"mysql":            {Service: "mysql", Endpoint: "mysql"},
		"mariadb":          {Service: "mariadb", Endpoint: "mysql"},
		"oracle-mysql":     {Service: "oracle-mysql", Endpoint: "mysql"},
		"chrome-headless":  {Service: "chrome-headless", Endpoint: "http"},
		"redis-persistent": {Service: "redis-persistent", Endpoint: "redis"},
	}, relationships(services))
}

func TestVersions(t *testing.T) {
	for _, c := range slices.Concat(runtimeChoices, serviceChoices) {
		assert.NotEmpty(t, versions(c.typ), c.typ)
	}
}
