package legacy

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// caBundle returns the certificates the legacy CLI should trust: those shipped
// with it, plus the trusted roots from the Windows certificate store.
//
// An organization which inspects TLS traffic installs its own root certificate
// in that store, and every other program on the machine then trusts it. The
// embedded PHP has to be given a CA file, because its openssl extension cannot
// read the store, so the store's certificates are added to the file instead.
//
// This needs curl in the embedded PHP to be built against OpenSSL. Built
// against Schannel, as static-php-cli does by default, it refuses a CA file
// larger than 1 MiB, which this bundle can exceed.
func caBundle() ([]byte, error) {
	roots, err := systemRootsPEM()
	if err != nil {
		return nil, err
	}

	bundle := make([]byte, 0, len(caCert)+len(roots)+1)
	bundle = append(bundle, bytes.TrimRight(caCert, "\n")...)
	bundle = append(bundle, '\n')
	return append(bundle, roots...), nil
}

// systemRootsPEM returns the trusted roots from the Windows certificate store
// which the shipped certificates do not already cover.
func systemRootsPEM() ([]byte, error) {
	shipped, err := certFingerprints(caCert)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	err = eachStoreCert("ROOT", func(der []byte) error {
		if shipped[sha256.Sum256(der)] {
			return nil
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			// A store can hold a certificate Go cannot read, and one which
			// cannot be parsed would make the whole file unusable.
			return nil
		}
		if now := time.Now(); now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return nil
		}
		return pem.Encode(&out, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	})
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// certFingerprints reads a PEM bundle and returns a fingerprint per certificate.
func certFingerprints(bundle []byte) (map[[32]byte]bool, error) {
	fingerprints := make(map[[32]byte]bool)
	for rest := bundle; len(rest) > 0; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			fingerprints[sha256.Sum256(block.Bytes)] = true
		}
	}
	if len(fingerprints) == 0 {
		return nil, errors.New("no certificates found in the shipped CA bundle")
	}
	return fingerprints, nil
}

// eachStoreCert calls fn with the encoded bytes of every certificate in a
// Windows system store, as read by every program which verifies against it.
func eachStoreCert(name string, fn func(der []byte) error) error {
	store, err := windows.CertOpenSystemStore(0, windows.StringToUTF16Ptr(name))
	if err != nil {
		return fmt.Errorf("could not open the %s certificate store: %w", name, err)
	}
	defer windows.CertCloseStore(store, 0) //nolint:errcheck

	var certContext *windows.CertContext
	for {
		certContext, err = windows.CertEnumCertificatesInStore(store, certContext)
		if certContext == nil {
			if err != nil && !errors.Is(err, windows.Errno(windows.CRYPT_E_NOT_FOUND)) {
				return fmt.Errorf("could not read the %s certificate store: %w", name, err)
			}
			return nil
		}
		// The context belongs to the store, so the bytes are only borrowed.
		if err := fn(unsafe.Slice(certContext.EncodedCert, certContext.Length)); err != nil {
			windows.CertFreeCertificateContext(certContext) //nolint:errcheck
			return err
		}
	}
}
