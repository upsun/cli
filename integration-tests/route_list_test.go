package tests

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestRouteList(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)

	projectID := mockapi.ProjectID()

	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Disk: 2048, Mounts: map[string]mockapi.Mount{}},
		},
		Routes: mockRoutes(),
		Links:  mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	envs := []*mockapi.Environment{main}

	apiHandler.SetEnvironments(envs)

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	assertTrimmed(t, `
+-------------------+----------+---------------------------+
| Route             | Type     | To                        |
+-------------------+----------+---------------------------+
| https://{default} | upstream | app:http                  |
| http://{default}  | redirect | https://main.example.com/ |
+-------------------+----------+---------------------------+
`, f.Run("routes", "-p", projectID, "-e", ".", "--refresh"))

	assert.Equal(t, "upstream\n", f.Run("route:get", "-p", projectID, "-e", ".", "https://{default}", "-P", "type"))
}

func TestRouteListLocal(t *testing.T) {
	t.Parallel()
	f := &cmdFactory{t: t}
	routes, err := json.Marshal(mockRoutes())
	require.NoError(t, err)
	f.extraEnv = []string{"PLATFORM_ROUTES=" + base64.StdEncoding.EncodeToString(routes)}

	assertTrimmed(t, `
+-------------------+----------+---------------------------+
| Route             | Type     | To                        |
+-------------------+----------+---------------------------+
| https://{default} | upstream | app:http                  |
| http://{default}  | redirect | https://main.example.com/ |
+-------------------+----------+---------------------------+
`, f.Run("route:list"))

	assert.Equal(t, "redirect\n", f.Run("route:get", "http://{default}", "--property", "type"))
	assert.Equal(t, "https://main.example.com/\n", f.Run("route:get", "http://{default}", "--property", "to"))
}

// TODO make a Route type
func mockRoutes() map[string]any {
	return map[string]any{
		"https://main.example.com/": map[string]any{
			"primary":        true,
			"id":             "app",
			"production_url": "https://main.example.com/",
			"attributes":     map[string]any{},
			"type":           "upstream",
			"original_url":   "https://{default}",
			"cache": map[string]any{
				"enabled":     true,
				"default_ttl": 0,
				"cookies":     []string{"*"},
				"headers":     []string{"Accept", "Accept-Language"},
			},
			"ssi": map[string]any{
				"enabled": false,
			},
			"upstream": "app:http",
		},
		"http://main.example.com/": map[string]any{
			"primary":        false,
			"id":             "app",
			"production_url": "http://main.example.com/",
			"attributes":     map[string]any{},
			"type":           "redirect",
			"original_url":   "http://{default}",
			"to":             "https://main.example.com/",
		},
	}
}

func TestRouteGetOptions(t *testing.T) {
	t.Parallel()
	withRoutes := func(t *testing.T, routes map[string]any) *cmdFactory {
		f := &cmdFactory{t: t}
		b, err := json.Marshal(routes)
		require.NoError(t, err)
		f.extraEnv = []string{"PLATFORM_ROUTES=" + base64.StdEncoding.EncodeToString(b)}
		return f
	}
	routes := map[string]any{
		"https://api.example.com/": map[string]any{
			"primary": false, "id": "api", "type": "upstream", "original_url": "https://api.{default}/", "upstream": "api:http",
		},
		"https://www.example.com/": map[string]any{
			"primary": true, "id": "main", "type": "upstream", "original_url": "https://www.{default}/", "upstream": "app:http",
		},
		"http://www.example.com/": map[string]any{
			"primary": false, "id": nil, "type": "redirect", "original_url": "http://www.{default}/",
			"to": "https://www.example.com/",
		},
	}
	f := withRoutes(t, routes)

	cases := []struct {
		name    string
		args    []string
		want    string
		wantErr string
	}{
		{name: "id", args: []string{"--id", "api"}, want: "https://api.{default}/\n"},
		{name: "primary", args: []string{"--primary"}, want: "https://www.{default}/\n"},
		{name: "original URL", args: []string{"http://www.{default}/"}, want: "http://www.{default}/\n"},
		{name: "unknown id", args: []string{"--id", "nope"}, wantErr: "No route found with ID: nope"},
		{name: "unknown original URL", args: []string{"https://nope.{default}/"},
			wantErr: "No route found for original URL: https://nope.{default}/"},
		{
			name:    "no selection",
			args:    nil,
			wantErr: "You must specify a route via the route argument, the --id option, or the --primary option.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := f.forSubtest(t)
			stdout, stderr, err := f.RunCombinedOutput(append([]string{"route:get", "-P", "original_url"}, c.args...)...)
			if c.wantErr != "" {
				assertExitCode(t, 1, err)
				assert.Contains(t, stderr, c.wantErr)
				return
			}
			require.NoError(t, err, stderr)
			assert.Equal(t, c.want, stdout)
		})
	}

	// Without a primary route.
	delete(routes, "https://www.example.com/")
	_, stderr, err := withRoutes(t, routes).RunCombinedOutput("route:get", "--primary")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "No primary route found.")
}
