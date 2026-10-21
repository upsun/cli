package legacy

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWindowsIniSetting checks that PHP reads a path back unchanged.
//
// PHP parses ini values as expressions, so an unquoted path is truncated at a
// character such as "~", which Windows short paths contain, and inside quotes a
// backslash escapes the next character.
func TestWindowsIniSetting(t *testing.T) {
	manager := newPHPManager(t.TempDir())
	require.NoError(t, manager.copy())

	cases := []struct {
		name string
		path string
	}{
		{"a long path", `C:\Users\runneradmin\AppData\Local\cacert.pem`},
		{"a short path", `C:\Users\RUNNER~1\AppData\Local\cacert.pem`},
		{"a network path", `\\server\share\cacert.pem`},
		{"a relative path", `certs\ca.pem`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := []string{"-n", "-d", iniSetting("openssl.cafile", c.path), "-r", `echo ini_get("openssl.cafile");`}
			output, err := exec.Command(manager.binPath(), args...).CombinedOutput() //nolint:gosec
			require.NoError(t, err, "PHP exited with an error: %s", output)
			assert.Equal(t, c.path, strings.TrimSpace(string(output)))
		})
	}
}

// TestWindowsSSLCertFile checks that curl in PHP verifies against SSL_CERT_FILE, when it is set, instead of the
// Windows certificate store.
func TestWindowsSSLCertFile(t *testing.T) {
	ca := generateTestCA(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{ca.serverCert}} //nolint:gosec // test server; Go picks the minimum version.
	server.StartTLS()
	defer server.Close()

	cacheDir := t.TempDir()
	manager := newPHPManager(cacheDir)
	require.NoError(t, manager.copy())

	writeCAFile := func(name string, certDER []byte) string {
		path := filepath.Join(cacheDir, name)
		require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o600))
		return path
	}
	serverCAFile := writeCAFile("server.pem", ca.certDER)
	otherCAFile := writeCAFile("other.pem", generateTestCA(t).certDER)

	cases := []struct {
		name     string
		certFile string
		want     string
	}{
		{"a file with the server's root", serverCAFile, "OK:hello"},
		{"a file without the server's root", otherCAFile, curlPeerFailedVerification},
		{"a missing file", filepath.Join(cacheDir, "missing.pem"), "ERR:77:"}, // CURLE_SSL_CACERT_BADFILE
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SSL_CERT_FILE", c.certFile)

			// Request the server as the legacy CLI does: with the wrapper's settings and best-effort revocation checks.
			args := []string{"-n"}
			for _, s := range manager.settings() {
				args = append(args, "-d", s)
			}
			args = append(args, "-r", phpRequestScript)
			cmd := exec.Command(manager.binPath(), args...) //nolint:gosec
			cmd.Env = append(os.Environ(), "TEST_URL="+server.URL, "TEST_SSL_OPTIONS="+strconv.Itoa(curlSSLOptRevokeBestEffort))
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "PHP exited with an error: %s", output)

			assert.True(t, strings.HasPrefix(string(output), c.want), "expected %q to start with %q", output, c.want)
		})
	}

	t.Run("no settings without SSL_CERT_FILE", func(t *testing.T) {
		t.Setenv("SSL_CERT_FILE", "")
		assert.Empty(t, manager.settings())
	})
}
