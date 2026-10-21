package legacy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

var procCertSetEnhancedKeyUsage = windows.NewLazySystemDLL("crypt32.dll").NewProc("CertSetEnhancedKeyUsage")

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

// TestWindowsCABundleDistrustedRoots checks that the roots of the compromised
// eMudhra intermediates are in neither the shipped certificates nor the bundle.
// Windows installs them on demand, so the store may not hold them yet: then
// only the shipped certificates are really checked.
func TestWindowsCABundleDistrustedRoots(t *testing.T) {
	roots := map[string]string{
		"AAA Certificate Services": "d7a7a0fb5d7e2731d771e9484ebcdef71d5f0c3e0a2948782bc83ee0ea699ef4",
		"emSign Root TLS CA - G3":  "7dd78d5f4f13459a83dff9abbba62edbaf6f2d102bf257fd712f4d9f2746ed8d",
		"emSign ECC Root CA - G3":  "86a1ecba089c4a8d3bbe2734c612ba341d813e043cf9e8a862cd5c57a36bbe6b",
	}

	inStore := make(map[[32]byte]bool)
	require.NoError(t, eachStoreCert("ROOT", func(_ *windows.CertContext, der []byte) error {
		inStore[sha256.Sum256(der)] = true
		return nil
	}))
	shipped, err := certFingerprints(caCert)
	require.NoError(t, err)
	bundle, err := caBundle()
	require.NoError(t, err)
	held, err := certFingerprints(bundle)
	require.NoError(t, err)

	for name, fingerprintHex := range roots {
		require.True(t, distrustedRoots[fingerprintHex], "expected %s to be distrusted", name)
		var fingerprint [32]byte
		_, err := hex.Decode(fingerprint[:], []byte(fingerprintHex))
		require.NoError(t, err)
		t.Logf("%s: in the store: %t", name, inStore[fingerprint])
		assert.False(t, shipped[fingerprint], "expected %s not to be shipped", name)
		assert.False(t, held[fingerprint], "expected %s not to be in the bundle", name)
	}
}

// TestWindowsCertificatePurposes checks which certificates count as TLS roots.
// A certificate is valid only for the purposes named by both its EKU extension
// and its store property, and one which names neither is valid for all of them.
func TestWindowsCertificatePurposes(t *testing.T) {
	const (
		serverAuth  = "1.3.6.1.5.5.7.3.1"
		codeSigning = "1.3.6.1.5.5.7.3.3"
		gatedCrypto = "1.3.6.1.4.1.311.10.3.3"
	)

	cases := []struct {
		name      string
		extension []x509.ExtKeyUsage
		property  []string
		allowed   bool
	}{
		{
			name:    "no purposes at all",
			allowed: true,
		},
		{
			name:      "an extension for code signing",
			extension: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
			allowed:   false,
		},
		{
			name:      "an extension for code signing and server authentication",
			extension: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning, x509.ExtKeyUsageServerAuth},
			allowed:   true,
		},
		{
			name:      "an extension for any purpose",
			extension: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
			allowed:   true,
		},
		{
			name:     "a property for code signing",
			property: []string{codeSigning},
			allowed:  false,
		},
		{
			name:     "a property for code signing and server authentication",
			property: []string{codeSigning, serverAuth},
			allowed:  true,
		},
		{
			name:     "a property for server gated crypto",
			property: []string{gatedCrypto},
			allowed:  true,
		},
		{
			name:      "a property which withdraws the extension's server authentication",
			extension: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			property:  []string{codeSigning},
			allowed:   false,
		},
		{
			name:      "a property which keeps the extension's server authentication",
			extension: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageCodeSigning},
			property:  []string{serverAuth},
			allowed:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, certContext := createTestCertContext(t, c.extension)
			if len(c.property) > 0 {
				setWindowsCertificatePurposes(t, certContext, c.property)
			}

			allowed, err := certAllowsServerAuth(certContext)
			require.NoError(t, err)
			assert.Equal(t, c.allowed, allowed)
		})
	}
}

func TestWindowsUsableTLSRoot(t *testing.T) {
	der, certContext := createTestCertContext(t, nil)
	usable, err := usableTLSRoot(certContext, der)
	require.NoError(t, err)
	assert.True(t, usable, "expected a valid certificate with no restriction to be a usable root")

	fingerprint := sha256.Sum256(der)
	distrustedRoots[hex.EncodeToString(fingerprint[:])] = true
	t.Cleanup(func() { delete(distrustedRoots, hex.EncodeToString(fingerprint[:])) })
	usable, err = usableTLSRoot(certContext, der)
	require.NoError(t, err)
	assert.False(t, usable, "expected a distrusted root not to be usable")

	// An EKU extension which cannot be decoded. Windows tolerates it, reporting
	// the purposes it could read rather than an error, so what keeps such a
	// certificate out of the bundle is Go refusing to parse it. The reason is
	// not asserted, only that it is never trusted.
	undecodable := pkix.Extension{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Value: []byte{0xff, 0xff}}
	der, certContext = createTestCertContext(t, nil, undecodable)
	usable, err = usableTLSRoot(certContext, der)
	t.Logf("an undecodable EKU extension: usable=%t, err=%v", usable, err)
	assert.False(t, usable, "expected a certificate with an undecodable EKU extension not to be trusted")
}

// createTestCertContext creates a context for a self-signed CA, which is given
// an EKU extension only when usages are passed, and returns its encoding too.
func createTestCertContext(t *testing.T, usages []x509.ExtKeyUsage, extra ...pkix.Extension) ([]byte, *windows.CertContext) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Upsun CLI test CA (safe to delete)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		ExtKeyUsage:           usages,
		ExtraExtensions:       extra,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	certContext, err := windows.CertCreateCertificateContext(
		windows.X509_ASN_ENCODING,
		&der[0],
		uint32(len(der)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, windows.CertFreeCertificateContext(certContext))
	})
	return der, certContext
}

// setWindowsCertificatePurposes replaces a certificate's EKU property, which is
// what restricting a root's purposes in the certificate manager writes, and
// which exporting the certificate cannot preserve.
func setWindowsCertificatePurposes(t *testing.T, certContext *windows.CertContext, identifiers []string) {
	t.Helper()

	oids := make([]*byte, len(identifiers))
	for i, identifier := range identifiers {
		oid, err := windows.BytePtrFromString(identifier)
		require.NoError(t, err)
		oids[i] = oid
	}
	usage := windows.CertEnhKeyUsage{
		Length:           uint32(len(oids)),
		UsageIdentifiers: &oids[0],
	}
	result, _, callErr := procCertSetEnhancedKeyUsage.Call(
		uintptr(unsafe.Pointer(certContext)),
		uintptr(unsafe.Pointer(&usage)),
	)
	require.NotZero(t, result, "setting certificate purposes failed: %s", callErr)
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
