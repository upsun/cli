package legacy

// This file covers which certificates the embedded PHP trusts on Windows.
//
// It matters because an organization that inspects TLS traffic installs an
// extra root certificate in the Windows certificate store, and curl in the
// embedded PHP is built against Schannel, which can read that store. Passing
// curl a CA file makes it verify against that file alone, so the settings the
// wrapper chooses decide whether such certificates are seen at all. See
// https://github.com/upsun/cli/issues/110.
//
// The test installs a throwaway CA in the machine's root store, so it needs
// administrator rights and only runs when CLI_TEST_MODIFY_CERT_STORE is set.
// It removes the certificate again afterwards.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// phpRequestScript makes one HTTPS request and reports the curl result.
//
// Revocation checks are turned off because the test CA publishes no
// revocation list, which Schannel otherwise treats as a failure. This test is
// about which certificates are trusted, not about revocation.
const phpRequestScript = `$ch = curl_init(getenv('TEST_URL'));
curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
curl_setopt($ch, CURLOPT_SSL_OPTIONS, CURLSSLOPT_NO_REVOKE);
$body = curl_exec($ch);
echo curl_errno($ch) === 0 ? 'OK:' . $body : 'ERR:' . curl_errno($ch) . ':' . curl_error($ch);`

// curlPeerFailedVerification is CURLE_PEER_FAILED_VERIFICATION, reported as
// "cURL error 60" by the CLI.
const curlPeerFailedVerification = "ERR:60:"

func TestWindowsCertStoreTrust(t *testing.T) {
	if os.Getenv("CLI_TEST_MODIFY_CERT_STORE") != "1" {
		t.Skip("set CLI_TEST_MODIFY_CERT_STORE=1, and run as administrator, to let this test add a certificate to the root store")
	}

	ca := generateTestCA(t)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{ca.serverCert}} //nolint:gosec // test server; Go picks the minimum version.
	server.StartTLS()
	defer server.Close()

	installCAInRootStore(t, ca.certDER)

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
			name:      "no CA file, so the store is used",
			args:      nil,
			wantTrust: true,
		},
		{
			// The wrapper passes this setting today, which is why an
			// organization's certificate is not seen. Expect this
			// expectation to change along with that.
			name:      "a CA file is used instead of the store",
			args:      []string{"-d", iniSetting("openssl.cafile", bundledCAFile)},
			wantTrust: false,
		},
		{
			name:      "a CA file which includes the store's certificate",
			args:      []string{"-d", iniSetting("openssl.cafile", mergedCAFile)},
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
		assert.True(t, rootStoreContains(t, ca.certDER),
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

// installCAInRootStore trusts a certificate and removes it when the test ends.
//
// It writes to the machine store, which needs administrator rights but no
// confirmation. Adding to the current user's store instead makes Windows ask
// the user to confirm, whether that is done through certutil or through the
// store API, so it cannot run unattended.
func installCAInRootStore(t *testing.T, certDER []byte) {
	t.Helper()

	store := openMachineRootStore(t)
	defer func() {
		assert.NoError(t, windows.CertCloseStore(store, 0))
	}()

	certContext, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING, &certDER[0], uint32(len(certDER)))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, windows.CertFreeCertificateContext(certContext))
	}()

	// Guard against the confirmation prompt: if one appears the call never
	// returns, and without this the whole test binary would time out.
	withoutHanging(t, "adding the certificate to the machine root store", func() error {
		return windows.CertAddCertificateContextToStore(store, certContext, windows.CERT_STORE_ADD_REPLACE_EXISTING, nil)
	})
	t.Log("installed the test CA in the machine root store")

	t.Cleanup(func() {
		removeCertFromRootStore(t, certDER)
	})
}

// removeCertFromRootStore deletes a certificate installed by the test.
func removeCertFromRootStore(t *testing.T, certDER []byte) {
	t.Helper()

	store := openMachineRootStore(t)
	defer func() {
		assert.NoError(t, windows.CertCloseStore(store, 0))
	}()

	found := eachCertInStore(t, store, func(certContext *windows.CertContext, encoded []byte) bool {
		if !bytes.Equal(encoded, certDER) {
			return false
		}
		// CertDeleteCertificateFromStore frees the context, so the
		// enumeration must stop here.
		assert.NoError(t, windows.CertDeleteCertificateFromStore(certContext))
		return true
	})
	assert.True(t, found, "expected to find the test CA in order to remove it")
}

// openMachineRootStore opens the machine's trusted root store for writing.
func openMachineRootStore(t *testing.T) windows.Handle {
	t.Helper()

	name, err := windows.UTF16PtrFromString("ROOT")
	require.NoError(t, err)
	store, err := windows.CertOpenStore(
		windows.CERT_STORE_PROV_SYSTEM,
		0,
		0,
		windows.CERT_SYSTEM_STORE_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(name)),
	)
	require.NoError(t, err, "opening the machine root store needs administrator rights")

	return store
}

// openRootStore opens the trusted roots as a program verifying a certificate
// would see them, which includes both the machine and user stores.
func openRootStore(t *testing.T) windows.Handle {
	t.Helper()

	name, err := windows.UTF16PtrFromString("ROOT")
	require.NoError(t, err)
	store, err := windows.CertOpenSystemStore(0, name)
	require.NoError(t, err)

	return store
}

// withoutHanging fails the test if fn does not return in time.
func withoutHanging(t *testing.T, description string, fn func() error) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()
	select {
	case err := <-done:
		require.NoError(t, err, description)
	case <-time.After(30 * time.Second):
		require.FailNow(t, "timed out "+description, "Windows may be waiting for confirmation, which cannot be given here")
	}
}

// eachCertInStore calls fn for each certificate until it returns true.
func eachCertInStore(t *testing.T, store windows.Handle, fn func(certContext *windows.CertContext, encoded []byte) bool) bool {
	t.Helper()

	var count int
	var certContext *windows.CertContext
	for {
		var err error
		certContext, err = windows.CertEnumCertificatesInStore(store, certContext)
		if certContext == nil {
			if err != nil && !errors.Is(err, windows.Errno(windows.CRYPT_E_NOT_FOUND)) {
				t.Logf("stopped reading the store: %s", err)
			}
			t.Logf("read %d certificates from the store", count)
			return false
		}
		count++
		encoded := unsafe.Slice(certContext.EncodedCert, certContext.Length)
		if fn(certContext, encoded) {
			t.Logf("matched a certificate after reading %d from the store", count)
			return true
		}
	}
}

// iniSetting quotes the value of a PHP setting. PHP reads ini values as
// expressions in which characters such as "~" are operators, and Windows short
// paths contain them, for example C:\Users\RUNNER~1\AppData.
func iniSetting(key, value string) string {
	return key + `="` + value + `"`
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

// rootStoreContains reports whether the trusted roots hold a certificate,
// using the same API a merged CA bundle would need to read them.
func rootStoreContains(t *testing.T, certDER []byte) bool {
	t.Helper()

	store := openRootStore(t)
	defer func() {
		assert.NoError(t, windows.CertCloseStore(store, 0))
	}()

	return eachCertInStore(t, store, func(_ *windows.CertContext, encoded []byte) bool {
		return bytes.Equal(encoded, certDER)
	})
}
