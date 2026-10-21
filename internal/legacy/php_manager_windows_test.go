package legacy

import (
	"crypto/tls"
	"encoding/pem"
	"io"
	"net"
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

// TestWindowsSSLCertFileSetting checks that PHP reads SSL_CERT_FILE back unchanged, whatever characters the path has.
func TestWindowsSSLCertFileSetting(t *testing.T) {
	manager := newPHPManager(t.TempDir())
	require.NoError(t, manager.copy())

	paths := []string{
		`C:\Users\runneradmin\AppData\Local\cacert.pem`,
		`C:\Users\RUNNER~1\AppData\Local\cacert.pem`,
		`\\server\share\cacert.pem`,
		`certs\ca.pem`,
		`C:\certs\${NOT_A_VAR}\$HOME;"x"\ca.pem`,
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Setenv("SSL_CERT_FILE", path)
			args := []string{"-n"}
			for _, s := range manager.settings() {
				args = append(args, "-d", s)
			}
			args = append(args, "-r", `echo ini_get("openssl.cafile");`)
			output, err := exec.Command(manager.binPath(), args...).CombinedOutput() //nolint:gosec
			require.NoError(t, err, "PHP exited with an error: %s", output)
			assert.Equal(t, path, string(output))
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

	// An HTTPS proxy, with a certificate from the same CA.
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(serveConnect))
	proxy.TLS = &tls.Config{Certificates: []tls.Certificate{ca.serverCert}} //nolint:gosec // test server; Go picks the minimum version.
	proxy.StartTLS()
	defer proxy.Close()

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
		env      []string
		want     string
	}{
		{"a file with the server's root", serverCAFile, nil, "OK:hello"},
		{"a file without the server's root", otherCAFile, nil, curlPeerFailedVerification},
		{"a missing file", filepath.Join(cacheDir, "missing.pem"), nil, "ERR:77:"}, // CURLE_SSL_CACERT_BADFILE
		{
			// So the legacy CLI sets CURLOPT_PROXY_CAINFO from openssl.cafile.
			name:     "an HTTPS proxy is not verified with the file by default",
			certFile: serverCAFile,
			env:      []string{"TEST_PROXY=" + proxy.URL},
			want:     curlPeerFailedVerification,
		},
		{
			name:     "an HTTPS proxy verified with the file",
			certFile: serverCAFile,
			env:      []string{"TEST_PROXY=" + proxy.URL, "TEST_PROXY_CAINFO_FROM_INI=1"},
			want:     "OK:hello",
		},
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
			cmd.Env = append(cmd.Env, c.env...)
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

// serveConnect is a minimal HTTPS proxy handler: it tunnels CONNECT requests.
func serveConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
		return
	}
	upstream, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	conn, buf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, buf)
		close(done)
	}()
	_, _ = io.Copy(conn, upstream)
	<-done
}
