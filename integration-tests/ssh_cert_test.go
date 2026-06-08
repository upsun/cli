package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestSSHCerts(t *testing.T) {
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"

	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	// The algorithm is set explicitly in each subtest so that expectations do
	// not depend on the runner's FIPS mode (which the "auto" default detects).
	t.Run("explicit ed25519", func(t *testing.T) {
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.extraEnv = []string{EnvPrefix + "SSH_CERT_KEY_ALGORITHM=ed25519"}

		output := f.Run("ssh-cert:info")
		assert.Regexp(t, `(?m)^filename: .+?id_ed25519-cert\.pub$`, output)
		assert.Contains(t, output, "key_id: test-key-id\n")
		assert.Contains(t, output, "key_type: ssh-ed25519-cert-v01@openssh.com\n")
	})

	// An explicit algorithm (e.g. for FIPS hosts where ed25519 is unavailable)
	// produces a certificate over that key type.
	t.Run("explicit rsa", func(t *testing.T) {
		f := newCommandFactory(t, apiServer.URL, authServer.URL)
		f.extraEnv = []string{EnvPrefix + "SSH_CERT_KEY_ALGORITHM=rsa"}

		output := f.Run("ssh-cert:info")
		assert.Regexp(t, `(?m)^filename: .+?id_rsa-cert\.pub$`, output)
		assert.Contains(t, output, "key_id: test-key-id\n")
		assert.Contains(t, output, "key_type: ssh-rsa-cert-v01@openssh.com\n")
	})
}
