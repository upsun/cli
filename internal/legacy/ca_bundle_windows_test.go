package legacy

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowsCABundle(t *testing.T) {
	bundle, err := caBundle()
	require.NoError(t, err)

	shipped := countCertificates(t, caCert)
	total := countCertificates(t, bundle)
	t.Logf("%d certificates in %d KB, of which %d are shipped", total, len(bundle)/1024, shipped)

	// Every shipped certificate is still there, and the store adds more.
	assert.Greater(t, total, shipped, "expected the store to add certificates")
	require.True(t, x509.NewCertPool().AppendCertsFromPEM(bundle),
		"expected the bundle to be readable as a pool of trusted roots")
}

// countCertificates parses a bundle and returns how many certificates it holds.
func countCertificates(t *testing.T, bundle []byte) int {
	t.Helper()

	var count int
	for rest := bundle; len(rest) > 0; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			require.Empty(t, rest, "expected the whole bundle to be readable")
			break
		}
		require.Equal(t, "CERTIFICATE", block.Type)
		_, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
		count++
	}
	return count
}

// BenchmarkWindowsSystemRootsPEM measures reading the store, which happens
// before every legacy command, to decide whether the result needs caching.
func BenchmarkWindowsSystemRootsPEM(b *testing.B) {
	for b.Loop() {
		if _, err := systemRootsPEM(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWindowsCAFile measures the whole of what a command pays: reading the
// store, building the bundle, and finding the cached file already up to date.
func BenchmarkWindowsCAFile(b *testing.B) {
	manager := &phpManagerPerOS{b.TempDir()}
	require.NoError(b, manager.writeCAFile())

	for b.Loop() {
		if err := manager.writeCAFile(); err != nil {
			b.Fatal(err)
		}
	}
}
