package legacy

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

var procCertAddEnhancedKeyUsageIdentifier = windows.NewLazySystemDLL("crypt32.dll").NewProc("CertAddEnhancedKeyUsageIdentifier")

func TestWindowsCABundle(t *testing.T) {
	bundle, err := caBundle()
	require.NoError(t, err)

	shipped, err := certFingerprints(caCert)
	require.NoError(t, err)
	held, err := certFingerprints(bundle)
	require.NoError(t, err)

	// The size varies by machine, so it is reported rather than asserted. It
	// only has to be a size curl will read, which OpenSSL does not limit.
	t.Logf("bundle: %d certificates in %d KB (%d shipped, %d from the store)",
		len(held), len(bundle)/1024, len(shipped), len(held)-len(shipped))

	assert.Greater(t, len(held), len(shipped), "expected the store to add certificates")
	for fingerprint := range shipped {
		require.True(t, held[fingerprint], "expected every shipped certificate to still be trusted")
	}
	require.True(t, x509.NewCertPool().AppendCertsFromPEM(bundle),
		"expected the bundle to be readable as a pool of trusted roots")
	for rest := bundle; len(rest) > 0; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			require.Empty(t, rest, "expected the whole bundle to be readable")
			break
		}
		_, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err, "expected every certificate in the bundle to be readable")
	}
}

func TestWindowsCertificatePurposes(t *testing.T) {
	ca := generateTestCA(t)
	certContext, err := windows.CertCreateCertificateContext(
		windows.X509_ASN_ENCODING,
		&ca.certDER[0],
		uint32(len(ca.certDER)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, windows.CertFreeCertificateContext(certContext))
	})

	allowed, err := certAllowsServerAuth(certContext)
	require.NoError(t, err)
	assert.True(t, allowed, "a certificate without an EKU restriction is valid for every purpose")

	addWindowsCertificatePurpose(t, certContext, "1.3.6.1.5.5.7.3.3") // Code Signing.
	allowed, err = certAllowsServerAuth(certContext)
	require.NoError(t, err)
	assert.False(t, allowed, "a certificate restricted to code signing must not become a TLS root")

	addWindowsCertificatePurpose(t, certContext, "1.3.6.1.5.5.7.3.1") // Server Authentication.
	allowed, err = certAllowsServerAuth(certContext)
	require.NoError(t, err)
	assert.True(t, allowed, "a certificate which permits server authentication is a TLS root")
}

func addWindowsCertificatePurpose(t *testing.T, certContext *windows.CertContext, identifier string) {
	t.Helper()

	oid, err := windows.BytePtrFromString(identifier)
	require.NoError(t, err)
	result, _, callErr := procCertAddEnhancedKeyUsageIdentifier.Call(
		uintptr(unsafe.Pointer(certContext)),
		uintptr(unsafe.Pointer(oid)),
	)
	require.NotZero(t, result, "adding certificate purpose failed: %s", callErr)
}

// BenchmarkWindowsCABundle measures reading the store and building the bundle,
// which happens before every legacy command.
func BenchmarkWindowsCABundle(b *testing.B) {
	for b.Loop() {
		if _, err := caBundle(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWindowsCAFile measures the whole of what a command pays: building
// the bundle, and finding the cached file already up to date.
func BenchmarkWindowsCAFile(b *testing.B) {
	manager := &phpManagerPerOS{cacheDir: b.TempDir()}
	require.NoError(b, manager.writeCAFile())

	for b.Loop() {
		if err := manager.writeCAFile(); err != nil {
			b.Fatal(err)
		}
	}
}
