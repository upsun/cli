package tests

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

func TestMountDownloadToNewRelativeTarget(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	remoteDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(remoteDir, "var", "log"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(remoteDir, "var", "log", "app.log"), []byte("log"), 0o644))

	sshServer, err := mockssh.NewServer(t, authServer.URL+"/ssh/authority")
	require.NoError(t, err)
	sshServer.CommandHandler = mockssh.ExecHandler(remoteDir, os.Environ())
	t.Cleanup(func() {
		if err := sshServer.Stop(); err != nil {
			t.Error(err)
		}
	})

	projectID := mockapi.ProjectID()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks("self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments"),
		DefaultBranch: "main",
	}})
	main := makeEnv(projectID, "main", "production", "active", nil)
	main.Links["pf:ssh:app"] = mockapi.HALLink{HREF: "ssh://app@ssh.cli-tests.example.com"}
	main.SetCurrentDeployment(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {
				Name: "app",
				Type: "golang:1.23",
				Size: "AUTO",
				Mounts: map[string]mockapi.Mount{
					"/var/log": {Source: "storage", SourcePath: "logs"},
				}},
		},
		Services: map[string]mockapi.Service{},
		Routes:   map[string]any{},
		Workers:  map[string]mockapi.Worker{},
		Links:    mockapi.MakeHALLinks("self=/projects/" + projectID + "/environments/main/deployment/current"),
	})
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.extraEnv = []string{
		EnvPrefix + "SSH_OPTIONS=HostName 127.0.0.1\nPort " + strconv.Itoa(sshServer.Port()),
		EnvPrefix + "SSH_HOST_KEYS=" + sshServer.HostKeyConfig(),
	}

	_, stdErr, err := f.RunCombinedOutput("mount:download", "-p", projectID, "-e", "main", "-m", "var/log",
		"--target", "dl", "--yes")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Downloading files from the remote mount var/log to dl")
	assert.FileExists(t, filepath.Join(f.dir, "dl", "app.log"))

	_, stdErr, err = f.RunCombinedOutput("mount:download", "-p", projectID, "-e", "main", "--all",
		"--target", "dl-all", "--yes")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Downloading files from var/log to dl-all/var/log")
	assert.FileExists(t, filepath.Join(f.dir, "dl-all", "var", "log", "app.log"))

	_, stdErr, err = f.RunCombinedOutput("mount:download", "-p", projectID, "-e", "main", "--all", "--source-path",
		"--target", "dl-source", "--yes")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Downloading files from var/log to dl-source/logs")
	assert.FileExists(t, filepath.Join(f.dir, "dl-source", "logs", "app.log"))
}
