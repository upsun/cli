package tests

import (
	"io/fs"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestSSHCerts(t *testing.T) {
	cases := []struct {
		name      string
		algorithm string
		filename  string
		keyType   string
	}{
		{"default", "", "id_ed25519", "ssh-ed25519-cert-v01@openssh.com"},
		{"rsa", "rsa", "id_rsa", "ssh-rsa-cert-v01@openssh.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()

			myUserID := "my-user-id"

			apiHandler := mockapi.NewHandler(t)
			apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			if c.algorithm != "" {
				f.extraEnv = []string{EnvPrefix + "SSH_CERT_KEY_ALGORITHM=" + c.algorithm}
			}

			output := f.Run("ssh-cert:info")
			assert.Regexp(t, `(?m)^filename: .+?`+c.filename+`-cert\.pub$`, output)
			assert.Contains(t, output, "key_id: test-key-id\n")
			assert.Contains(t, output, "key_type: "+c.keyType+"\n")
		})
	}
}

func TestSSHCertAlgorithmSwitch(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: "my-user-id"})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.home = t.TempDir()

	assert.Contains(t, f.Run("ssh-cert:info"), "key_type: ssh-ed25519-cert-v01@openssh.com\n")

	f.extraEnv = []string{EnvPrefix + "SSH_CERT_KEY_ALGORITHM=rsa"}
	assert.Contains(t, f.Run("ssh-cert:info"), "key_type: ssh-rsa-cert-v01@openssh.com\n")

	// The ed25519 key files should have been removed.
	var keyFiles []string
	err := filepath.WalkDir(f.home, func(path string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), "id_") {
			keyFiles = append(keyFiles, d.Name())
		}
		return err
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"id_rsa", "id_rsa.pub", "id_rsa-cert.pub"}, keyFiles)
}
