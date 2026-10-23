package tests

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

// TestEnvironmentDrush checks the remote Drush command: its Drupal root, site URL and arguments.
func TestEnvironmentDrush(t *testing.T) {
	t.Parallel()
	web := map[string]any{"locations": map[string]any{"/": map[string]any{"root": "public"}}}
	cases := []struct {
		name      string
		args      []string
		remoteEnv []string
		web       map[string]any
		noRoutes  bool
		want      []string
	}{
		{
			name:      "document root",
			remoteEnv: []string{"PLATFORM_DOCUMENT_ROOT=/app/web"},
			want:      []string{"--root=/app/web", "--uri=https://main.example.com/", "status"},
		},
		{
			name:      "app dir and web root",
			remoteEnv: []string{"PLATFORM_APP_DIR=/srv/app"},
			web:       web,
			want:      []string{"--root=/srv/app/public", "--uri=https://main.example.com/", "status"},
		},
		{
			name:     "default app dir, without routes",
			web:      web,
			noRoutes: true,
			want:     []string{"--root=/app/public", "status"},
		},
		{
			name: "arguments",
			args: []string{"user-login", "--", "--mail=a b@example.com"},
			want: []string{"--root=/app/", "--uri=https://main.example.com/", "user-login", "--mail=a b@example.com"},
		},
		{
			name: "quoted command",
			args: []string{"en overlay"},
			want: []string{"--root=/app/", "--uri=https://main.example.com/", "en", "overlay"},
		},
		{
			name: "shared options",
			args: []string{"cr", "--yes", "-v"},
			want: []string{"--root=/app/", "--uri=https://main.example.com/", "cr", "--yes", "--verbose"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := &mockapi.Deployment{
				WebApps: map[string]mockapi.App{"app": {Name: "app", Type: "php:8.4", Size: "M", Web: c.web}},
			}
			if !c.noRoutes {
				d.Routes = mockRoutes()
			}
			p := setUpProject(t, sshEnv(d))
			// The fake drush prints COLUMNS and its arguments, one per line.
			env := remoteEnv(t, map[string]string{"drush": `echo "COLUMNS=$COLUMNS"; printf '%s\n' "$@"`}, nil)
			p.serveSSH(t, mockssh.ExecHandler(t.TempDir(), append(env, c.remoteEnv...)))

			args := append([]string{"environment:drush", "-p", layoutProjectID, "-e", "main"}, c.args...)
			stdout, stdErr, err := p.f.RunCombinedOutput(args...)
			require.NoError(t, err, stdErr)
			want := append([]string{"COLUMNS=120"}, c.want...)
			assert.Equal(t, want, strings.Split(strings.TrimSuffix(stdout, "\n"), "\n"))
		})
	}
}
