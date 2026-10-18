package platformify

import (
	"maps"
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

				assert.NotEmpty(t, files)
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

func TestVersions(t *testing.T) {
	for _, c := range slices.Concat(runtimeChoices, serviceChoices) {
		assert.NotEmpty(t, versions(c.typ), c.typ)
	}
}

func TestGenerateSubdirectoryApp(t *testing.T) {
	cases := []struct {
		flex bool
		want []string
	}{
		{true, []string{".upsun/config.yaml", "app/.environment"}},
		{false, []string{".platform/routes.yaml", ".platform/services.yaml", "app/.environment", "app/.platform.app.yaml"}},
	}
	for _, c := range cases {
		in := testInputs()["generic"]
		in.Assets = &assets{flex: c.flex}
		in.ApplicationRoot = "/app"
		files, err := generate(t.TempDir(), in)
		require.NoError(t, err)
		assert.ElementsMatch(t, c.want, slices.Collect(maps.Keys(files)))
	}
}

func TestGenerateRedisPersistent(t *testing.T) {
	in := testInputs()["generic"]
	in.Services = []service{{Name: "redis-persistent", Type: "redis-persistent:8.0"}}
	for _, flex := range []bool{true, false} {
		in.Assets = &assets{flex: flex}
		files, err := generate(t.TempDir(), in)
		require.NoError(t, err)
		assert.Contains(t, string(files[".environment"]), `export REDIS_URL="$CACHE_URL"`)
		if !flex {
			assert.Contains(t, string(files[".environment"]), `jq -r '.["redis-persistent"][0].host'`)
		}
	}
}

func TestGenerateStrapiDatabaseScheme(t *testing.T) {
	in := testInputs()["strapi"]
	in.Services = []service{{Name: "postgresql", Type: "postgresql:18"}}
	for _, flex := range []bool{true, false} {
		in.Assets = &assets{flex: flex}
		files, err := generate(t.TempDir(), in)
		require.NoError(t, err)
		assert.Contains(t, string(files[".environment"]), `export DATABASE_SCHEME="$DB_SCHEME"`)
	}
}

func TestExistingConfig(t *testing.T) {
	cases := []struct {
		name          string
		flex          bool
		files         map[string]string
		wantOverwrite []string
		wantConflicts []string
	}{
		{
			name: "fixed app moved to a subdirectory",
			files: map[string]string{
				"app/composer.lock":           "{}",
				".platform.app.yaml":          "name: app\n",
				".platform/applications.yaml": "[]\n",
			},
			wantConflicts: []string{".platform.app.yaml", ".platform/applications.yaml"},
		},
		{
			name:          "fixed app overwritten",
			files:         map[string]string{"app/composer.lock": "{}", "app/.platform.app.yaml": "name: app\n"},
			wantOverwrite: []string{"app/.platform.app.yaml"},
		},
		{
			name:          "flex",
			flex:          true,
			files:         map[string]string{".upsun/config.yaml": "applications: {}\n", ".upsun/other.yml": "services: {}\n"},
			wantOverwrite: []string{".upsun/config.yaml"},
			wantConflicts: []string{".upsun/other.yml"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := writeTree(t, c.files)
			in := testInputs()["generic"]
			in.Assets = &assets{flex: c.flex}
			in.ApplicationRoot = "/app"
			files, err := generate(dir, in)
			require.NoError(t, err)
			w := &wizard{dir: dir, assets: in.Assets}
			overwrite, conflicts := w.existingConfig(files)
			assert.Equal(t, c.wantOverwrite, overwrite)
			assert.Equal(t, c.wantConflicts, conflicts)
		})
	}
}
