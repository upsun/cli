package legacy

// This file answers two questions about certificate trust on Windows, which
// decide how the CLI should configure its CA bundle there:
//
//  1. Does a CA bundle stop the embedded PHP from using the Windows
//     certificate store? The store is where an organization installs the extra
//     root certificate needed when it inspects TLS traffic. curl in the
//     embedded PHP is built against Schannel, and its source suggests that
//     passing any CA file makes it verify against that file alone.
//
//  2. Can a CA bundle which includes the store's certificates restore trust,
//     and can the store be read from Go in the first place?
//
// The test installs a throwaway CA in the current user's root store, so it
// only runs when CLI_TEST_MODIFY_CERT_STORE is set. It cleans up afterwards.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // Windows identifies certificates by SHA-1 thumbprint.
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phpRequestScript makes one HTTPS request and reports the curl result.
const phpRequestScript = `$ch = curl_init(getenv('TEST_URL'));
curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
$body = curl_exec($ch);
echo curl_errno($ch) === 0 ? 'OK:' . $body : 'ERR:' . curl_errno($ch) . ':' . curl_error($ch);`

// curlPeerFailedVerification is CURLE_PEER_FAILED_VERIFICATION, reported as
// "cURL error 60" by the CLI.
const curlPeerFailedVerification = "ERR:60:"

func TestWindowsCertStoreTrust(t *testing.T) {
	if os.Getenv("CLI_TEST_MODIFY_CERT_STORE") != "1" {
		t.Skip("set CLI_TEST_MODIFY_CERT_STORE=1 to let this test add a certificate to the user's root store")
	}

	ca := generateTestCA(t)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{ca.serverCert}} //nolint:gosec // test server; Go picks the minimum version.
	server.StartTLS()
	defer server.Close()

	installCAInUserRootStore(t, ca.certDER)

	// Lay out the PHP binary and its bundled CA file the way the CLI does.
	cacheDir := t.TempDir()
	manager := newPHPManager(cacheDir)
	require.NoError(t, manager.copy())
	phpBin := manager.binPath()
	bundledCAFile := filepath.Join(cacheDir, "cacert.pem")
	require.FileExists(t, bundledCAFile)

	// A bundle holding both the shipped certificates and the one from the
	// store: what the wrapper would have to generate to keep organization
	// certificates working.
	mergedCAFile := filepath.Join(cacheDir, "merged.pem")
	bundled, err := os.ReadFile(bundledCAFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(mergedCAFile, append(bundled, ca.certPEM...), 0o600))

	cases := []struct {
		name      string
		args      []string
		wantTrust bool
	}{
		{
			name:      "no CA file, so the Windows certificate store is used",
			args:      nil,
			wantTrust: true,
		},
		{
			name:      "the bundled CA file replaces the store, as the wrapper configures today",
			args:      []string{"-d", "openssl.cafile=" + bundledCAFile},
			wantTrust: false,
		},
		{
			name:      "a CA file including the store's certificate restores trust",
			args:      []string{"-d", "openssl.cafile=" + mergedCAFile},
			wantTrust: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			output := runPHPRequest(t, phpBin, server.URL, c.args...)
			t.Logf("php %v -> %s", c.args, output)
			if c.wantTrust {
				assert.Equal(t, "OK:hello", output)
			} else {
				assert.Contains(t, output, curlPeerFailedVerification)
			}
		})
	}

	t.Run("the root store can be read from Go", func(t *testing.T) {
		assert.True(t, userRootStoreContains(t, ca.certDER),
			"expected to find the installed certificate by enumerating the ROOT store")
	})
}

type testCA struct {
	certPEM    []byte
	certDER    []byte
	serverCert tls.Certificate
}

// generateTestCA creates a CA and a certificate for 127.0.0.1 signed by it.
func generateTestCA(t *testing.T) testCA {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(24 * time.Hour)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Upsun CLI test CA (safe to delete)"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	return testCA{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		certDER: caDER,
		serverCert: tls.Certificate{
			Certificate: [][]byte{serverDER, caDER},
			PrivateKey:  serverKey,
		},
	}
}

// installCAInUserRootStore trusts a certificate for the current user only,
// which needs no elevation, and removes it when the test ends.
func installCAInUserRootStore(t *testing.T, certDER []byte) {
	t.Helper()

	certFile := filepath.Join(t.TempDir(), "test-ca.cer")
	require.NoError(t, os.WriteFile(certFile, certDER, 0o600))
	runCertutil(t, "-user", "-f", "-addstore", "Root", certFile)

	sum := sha1.Sum(certDER) //nolint:gosec // Windows identifies certificates by SHA-1 thumbprint.
	thumbprint := strings.ToUpper(hex.EncodeToString(sum[:]))
	t.Cleanup(func() {
		runCertutil(t, "-user", "-f", "-delstore", "Root", thumbprint)
	})
}

func runCertutil(t *testing.T, args ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "certutil", args...).CombinedOutput()
	require.NoError(t, err, "certutil %v failed: %s", args, output)
	t.Logf("certutil %v -> %s", args, strings.TrimSpace(string(output)))
}

func runPHPRequest(t *testing.T, phpBin, url string, extraArgs ...string) string {
	t.Helper()

	args := append([]string{"-n"}, extraArgs...)
	args = append(args, "-r", phpRequestScript)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, phpBin, args...)
	cmd.Env = append(os.Environ(), "TEST_URL="+url)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "PHP exited with an error: %s", output)

	return strings.TrimSpace(string(output))
}

// userRootStoreContains reports whether the ROOT store holds a certificate,
// using the same API a merged CA bundle would need to read it.
func userRootStoreContains(t *testing.T, certDER []byte) bool {
	t.Helper()

	name, err := syscall.UTF16PtrFromString("ROOT")
	require.NoError(t, err)
	store, err := syscall.CertOpenSystemStore(0, name)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, syscall.CertCloseStore(store, 0))
	}()

	var count int
	var context *syscall.CertContext
	for {
		context, err = syscall.CertEnumCertificatesInStore(store, context)
		if err != nil || context == nil {
			t.Logf("read %d certificates from the ROOT store", count)
			return false
		}
		count++
		encoded := unsafe.Slice(context.EncodedCert, context.Length)
		if bytes.Equal(encoded, certDER) {
			t.Logf("found the installed certificate after reading %d from the ROOT store", count)
			return true
		}
	}
}
