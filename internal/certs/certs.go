// Package certs lets the Go part of the CLI trust the certificates named by
// SSL_CERT_FILE, which the legacy PHP part already trusts on every platform.
package certs

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"runtime"
)

// EnvVar names a file holding the certificates to trust.
const EnvVar = "SSL_CERT_FILE"

// UseEnvCertFile makes the default HTTP transport verify against the
// certificates named by SSL_CERT_FILE.
//
// Go reads that variable itself on Unix, but not on Windows or macOS, where it
// verifies through the operating system instead. The legacy CLI reads it on
// every platform, through Composer\CaBundle, so without this the two parts of
// the CLI disagree about which certificates to trust.
//
// The file replaces the system certificates rather than adding to them, which
// is what Go does on Unix and what the legacy CLI does everywhere.
func UseEnvCertFile() error {
	if goReadsEnvCertFile() {
		return nil
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("the default HTTP transport cannot be configured")
	}
	return useCertFile(transport, os.Getenv(EnvVar))
}

// goReadsEnvCertFile reports whether Go's own verification reads the variable.
func goReadsEnvCertFile() bool {
	return runtime.GOOS != "windows" && runtime.GOOS != "darwin"
}

// useCertFile points a transport at a certificate file. An empty path is
// ignored, leaving the system certificates in use.
func useCertFile(transport *http.Transport, path string) error {
	if path == "" {
		return nil
	}
	pemCerts, err := os.ReadFile(path) //nolint:gosec // the user names the file.
	if err != nil {
		return fmt.Errorf("could not read %s: %w", EnvVar, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemCerts) {
		return fmt.Errorf("no certificates found in %s: %s", EnvVar, path)
	}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	transport.TLSClientConfig.RootCAs = pool
	return nil
}
