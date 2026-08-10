package certs

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUseCertFile checks that a server signed by a certificate in the file is
// trusted, and that one signed by an unrelated certificate is not.
func TestUseCertFile(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	certFile := filepath.Join(t.TempDir(), "cert.pem")
	require.NoError(t, os.WriteFile(certFile, serverCertPEM(t, server), 0o600))

	cases := []struct {
		name      string
		path      string
		wantTrust bool
	}{
		{"no file, so the system certificates are used", "", false},
		{"a file holding the server's certificate", certFile, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			transport := &http.Transport{}
			require.NoError(t, useCertFile(transport, c.path))

			resp, err := (&http.Client{Transport: transport}).Get(server.URL)
			if !c.wantTrust {
				require.Error(t, err, "expected the certificate not to be trusted")
				return
			}
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Setting the roots is what makes the file replace the system
			// certificates rather than add to them.
			assert.NotNil(t, transport.TLSClientConfig.RootCAs)
		})
	}
}

func TestUseCertFileErrors(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.pem")
	require.NoError(t, os.WriteFile(empty, []byte("not a certificate"), 0o600))

	assert.ErrorContains(t, useCertFile(&http.Transport{}, filepath.Join(t.TempDir(), "missing.pem")),
		"could not read SSL_CERT_FILE")
	assert.ErrorContains(t, useCertFile(&http.Transport{}, empty),
		"no certificates found in SSL_CERT_FILE")
}

// serverCertPEM returns the certificate a test server signs with.
func serverCertPEM(t *testing.T, server *httptest.Server) []byte {
	t.Helper()

	require.NotNil(t, server.Certificate())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
}
