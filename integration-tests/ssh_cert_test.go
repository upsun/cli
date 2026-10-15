package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestSSHCerts(t *testing.T) {
	t.Parallel()
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

func TestSSHCertLoad(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	myUserID := "my-user-id"
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stdErr, err := f.RunCombinedOutput("ssh-cert:info", "--no-refresh")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "No valid SSH certificate found.")

	// The CLI treats a certificate as expired 2 minutes early.
	authServer.SetSSHCertificate(myUserID, time.Minute)
	_, stdErr, err = f.RunCombinedOutput("ssh-cert:load", "--refresh-only")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Generating SSH certificate...")
	assert.NoFileExists(t, filepath.Join(f.home, ".ssh", "config"))

	_, stdErr, err = f.RunCombinedOutput("ssh-cert:info", "--no-refresh")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "No valid SSH certificate found.")

	// A certificate with the wrong key ID is not valid.
	authServer.SetSSHCertificate("other-user-id", time.Hour)
	f.Run("ssh-cert:load", "--new")
	_, _, err = f.RunCombinedOutput("ssh-cert:info", "--no-refresh")
	assertExitCode(t, 1, err)

	// Without --no-refresh, an invalid certificate is regenerated.
	authServer.SetSSHCertificate(myUserID, time.Hour)
	before := time.Now()
	output := f.Run("ssh-cert:info")
	assert.Contains(t, output, "key_id: "+myUserID+"\n")
	assert.Equal(t, 3, authServer.SSHCertificateCount())

	validBefore, err := strconv.ParseInt(
		strings.TrimSpace(f.Run("ssh-cert:info", "-P", "valid_before", "--date-fmt", "U")), 10, 64)
	require.NoError(t, err)
	assert.WithinDuration(t, before.Add(time.Hour), time.Unix(validBefore, 0), time.Minute)

	certFile := strings.TrimSpace(f.Run("ssh-cert:info", "-P", "filename"))
	keyFile := strings.TrimSpace(f.Run("ssh-cert:info", "-P", "key_filename"))
	assert.Equal(t, keyFile+"-cert.pub", certFile)
	assert.Equal(t, 3, authServer.SSHCertificateCount(), "a valid certificate is reused")

	// A valid certificate is reused, and the SSH configuration is written.
	_, stdErr, err = f.RunCombinedOutput("ssh-cert:load")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "A valid SSH certificate exists")
	assert.Equal(t, 3, authServer.SSHCertificateCount())
	userConfig := readFile(t, filepath.Join(f.home, ".ssh", "config"))
	assert.Contains(t, userConfig, "Host *.cli-tests.example.com\n")
	assert.Contains(t, userConfig, "Include "+filepath.Join(f.home, ".platform-test-cli", "ssh", "*.config"))
	assert.Contains(t, readFile(t, filepath.Join(f.home, ".platform-test-cli", "ssh", "session.config")), "Include ")
	sessionConfig := readFile(t, filepath.Join(filepath.Dir(certFile), "config"))
	assert.Contains(t, sessionConfig, "CertificateFile "+certFile)

	// --new refreshes the certificate, keeping the key.
	publicKey := readFile(t, keyFile+".pub")
	cert := readFile(t, certFile)
	_, stdErr, err = f.RunCombinedOutput("ssh-cert:load", "--new")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Generating SSH certificate...")
	assert.Equal(t, 4, authServer.SSHCertificateCount())
	assert.Equal(t, publicKey, readFile(t, keyFile+".pub"))
	assert.NotEqual(t, cert, readFile(t, certFile))

	// --new-key also generates a new key.
	_, stdErr, err = f.RunCombinedOutput("ssh-cert:load", "--new-key")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "The --new-key option is deprecated")
	assert.Equal(t, 5, authServer.SSHCertificateCount())
	assert.NotEqual(t, publicKey, readFile(t, keyFile+".pub"))
}

func readFile(t *testing.T, filename string) string {
	t.Helper()
	b, err := os.ReadFile(filename)
	require.NoError(t, err)
	return string(b)
}
