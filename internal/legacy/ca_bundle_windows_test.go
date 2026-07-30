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

	shipped, err := certFingerprints(caCert)
	require.NoError(t, err)
	held, err := certFingerprints(bundle)
	require.NoError(t, err)

	// What the machine holds, to show how much of the store is left out, and
	// why the whole store would not fit. This varies by machine, so it is
	// reported rather than asserted.
	root, err := storeFingerprints("ROOT")
	require.NoError(t, err)
	authRoot, err := storeFingerprints("AuthRoot")
	require.NoError(t, err)
	var inRootProgram int
	for fingerprint := range root {
		if authRoot[fingerprint] {
			inRootProgram++
		}
	}
	t.Logf("bundle: %d certificates in %d KB (%d shipped, %d added)",
		len(held), len(bundle)/1024, len(shipped), len(held)-len(shipped))
	t.Logf("store: %d roots, of which %d are in Microsoft's root program (AuthRoot holds %d)",
		len(root), inRootProgram, len(authRoot))

	assert.Less(t, len(bundle), maxCAFileSize, "expected curl to be willing to read the bundle")
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
	manager := &phpManagerPerOS{b.TempDir()}
	require.NoError(b, manager.writeCAFile())

	for b.Loop() {
		if err := manager.writeCAFile(); err != nil {
			b.Fatal(err)
		}
	}
}
