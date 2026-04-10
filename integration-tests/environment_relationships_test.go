package tests

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

func mockRelationships() map[string]any {
	return map[string]any{
		"database": []any{
			map[string]any{
				"service":  "db",
				"host":     "database.internal",
				"rel":      "mysql",
				"scheme":   "mysql",
				"username": "user",
				"password": "",
				"path":     "main",
				"port":     3306,
				"type":     "mysql:10.6",
			},
		},
		"redis": []any{
			map[string]any{
				"service":  "cache",
				"host":     "redis.internal",
				"rel":      "redis",
				"scheme":   "redis",
				"username": "",
				"password": "",
				"path":     "",
				"port":     6379,
				"type":     "redis:7.0",
			},
		},
	}
}

func TestRelationshipsLocal(t *testing.T) {
	data, err := json.Marshal(mockRelationships())
	require.NoError(t, err)

	f := &cmdFactory{t: t}
	f.extraEnv = []string{"PLATFORM_RELATIONSHIPS=" + base64.StdEncoding.EncodeToString(data)}

	// List all relationships.
	output := f.Run("environment:relationships")
	assert.Contains(t, output, "database")
	assert.Contains(t, output, "redis")

	// Extract a specific property (fully-qualified path: relationship.index.key).
	assertTrimmed(t, "database.internal", f.Run("environment:relationships", "-P", "database.0.host"))
	assertTrimmed(t, "redis.internal", f.Run("environment:relationships", "-P", "redis.0.host"))
	assertTrimmed(t, "3306", f.Run("environment:relationships", "-P", "database.0.port"))
}

func TestRelationshipsRemote(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	sshServer, err := mockssh.NewServer(t, authServer.URL+"/ssh/authority")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sshServer.Stop(); err != nil {
			t.Error(err)
		}
	})

	relJSON, err := json.Marshal(mockRelationships())
	require.NoError(t, err)
	sshServer.CommandHandler = mockssh.ExecHandler(t.TempDir(), []string{
		"PLATFORM_RELATIONSHIPS=" + base64.StdEncoding.EncodeToString(relJSON),
	})

	projectID := mockapi.ProjectID()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "my-user-id"})
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	mainEnv := makeEnv(projectID, "main", "production", "active", nil)
	mainEnv.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Disk: 2048, Mounts: map[string]mockapi.Mount{}},
		},
		Services: map[string]mockapi.App{},
		Workers:  map[string]mockapi.Worker{},
		Routes:   make(map[string]any),
		Links:    mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	mainEnv.Links["pf:ssh:app:0"] = mockapi.HALLink{HREF: "ssh://app--0@ssh.cli-tests.example.com"}
	apiHandler.SetEnvironments([]*mockapi.Environment{mainEnv})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = []string{
		EnvPrefix + "SSH_OPTIONS=HostName 127.0.0.1\nPort " + strconv.Itoa(sshServer.Port()),
		EnvPrefix + "SSH_HOST_KEYS=" + sshServer.HostKeyConfig(),
	}
	f.Run("cc")

	// List all relationships via SSH.
	output := f.Run("relationships", "-p", projectID, "-e", ".", "--refresh")
	assert.Contains(t, output, "database")
	assert.Contains(t, output, "redis")

	// Extract a property via SSH.
	assertTrimmed(t, "database.internal",
		f.Run("relationships", "-p", projectID, "-e", ".", "--refresh", "-P", "database.0.host"))
}
