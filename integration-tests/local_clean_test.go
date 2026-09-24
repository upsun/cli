package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestLocalClean checks that --keep and --max-age, which Symfony passes as
// strings, are accepted by local:clean.
func TestLocalClean(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiServer := httptest.NewServer(mockapi.NewHandler(t))
	defer apiServer.Close()

	cases := []struct {
		name     string
		args     []string
		wantErr  bool
		expected []string
	}{
		{"keep", []string{"--keep", "1"}, false, []string{"Deleted 2 build(s)", "Kept 1 build(s)"}},
		{"max-age", []string{"--max-age", "3600"}, false, []string{"Deleted 2 build(s)", "Kept 1 build(s)"}},
		{"invalid max-age", []string{"--max-age", "abc"}, true, []string{"The --max-age value must be an integer."}},
		{"invalid keep", []string{"--keep=-1"}, true, []string{"The --keep value must be an integer."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := initGitRepo(t)
			localDir := filepath.Join(repo, ".platform", "local")
			require.NoError(t, os.MkdirAll(localDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(localDir, "project.yaml"), []byte("id: abc123\n"), 0o644))

			// One recent build and two builds older than an hour.
			for i, age := range []time.Duration{0, 2 * time.Hour, 3 * time.Hour} {
				dir := filepath.Join(localDir, "builds", "build"+string(rune('a'+i)))
				require.NoError(t, os.MkdirAll(dir, 0o755))
				mtime := time.Now().Add(-age)
				require.NoError(t, os.Chtimes(dir, mtime, mtime))
			}

			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			f.dir = repo
			_, stdErr, err := f.RunCombinedOutput(append([]string{"local:clean"}, c.args...)...)
			if c.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "stderr: %s", stdErr)
			}
			for _, e := range c.expected {
				assert.Contains(t, stdErr, e)
			}
		})
	}
}
