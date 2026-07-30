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

// maxCAFileSize is the largest CA file curl will read on Windows, from
// MAX_CAFILE_SIZE in its schannel_verify.c. A larger file fails every request.
const maxCAFileSize = 1024 * 1024

// caBundle returns the certificates the legacy CLI should trust: those shipped
// with it, plus the roots installed locally on this machine.
//
// An organization which inspects TLS traffic installs its own root certificate
// in the Windows store, and every other program on the machine then trusts it.
// curl in the embedded PHP could read that store, but only while no CA file is
// set, and one has to be set because the openssl extension cannot read the
// store at all.
//
// Only the locally installed roots are added, not the whole store: Windows also
// keeps the certificates it gets from Microsoft's root program there, which are
// numerous enough to exceed the size curl will read. Leaving those out cannot
// lose any trust the CLI had before, because before it trusted only the
// certificates shipped with it.
func caBundle() ([]byte, error) {
	extra, err := localRootsPEM()
	if err != nil {
		return nil, err
	}

	bundle := make([]byte, 0, len(caCert)+len(extra)+1)
	bundle = append(bundle, bytes.TrimRight(caCert, "\n")...)
	bundle = append(bundle, '\n')
	bundle = append(bundle, extra...)

	if len(bundle) > maxCAFileSize {
		// curl would refuse the file, so no request would work at all. The
		// shipped certificates alone are what the CLI trusted before.
		return caCert, nil
	}
	return bundle, nil
}

// localRootsPEM returns the roots this machine was told to trust, meaning those
// in the store which are neither shipped with the CLI nor part of Microsoft's
// root program.
func localRootsPEM() ([]byte, error) {
	shipped, err := certFingerprints(caCert)
	if err != nil {
		return nil, err
	}
	// AuthRoot holds Microsoft's root program, which the shipped certificates
	// cover for the purposes of the CLI.
	publicRoots, err := storeFingerprints("AuthRoot")
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	err = eachStoreCert("ROOT", func(der []byte) error {
		fingerprint := sha256.Sum256(der)
		if shipped[fingerprint] || publicRoots[fingerprint] {
			return nil
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil // Not a certificate this build of Go can read.
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

// storeFingerprints returns a fingerprint per certificate in a Windows store.
func storeFingerprints(name string) (map[[32]byte]bool, error) {
	fingerprints := make(map[[32]byte]bool)
	err := eachStoreCert(name, func(der []byte) error {
		fingerprints[sha256.Sum256(der)] = true
		return nil
	})
	return fingerprints, err
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
