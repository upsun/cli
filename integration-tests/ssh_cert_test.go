package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestSSHCerts(t *testing.T) {
	cases := []struct {
		name      string
		algorithm string
		filename  string
		keyType   string
	}{
		{"ed25519", "ed25519", "id_ed25519", "ssh-ed25519-cert-v01@openssh.com"},
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
			f.extraEnv = []string{EnvPrefix + "SSH_CERT_KEY_ALGORITHM=" + c.algorithm}

			output := f.Run("ssh-cert:info")
			assert.Regexp(t, `(?m)^filename: .+?`+c.filename+`-cert\.pub$`, output)
			assert.Contains(t, output, "key_id: test-key-id\n")
			assert.Contains(t, output, "key_type: "+c.keyType+"\n")
		})
	}
}
