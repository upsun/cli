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
// It always returns a usable bundle. If any of the store could not be read it
// also returns the reason, having left out only what it could not read: the
// shipped certificates are what the CLI trusted before, so they are still
// usable, and the caller decides what to do about the rest.
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

	bundle := make([]byte, 0, len(caCert)+len(roots)+1)
	bundle = append(bundle, bytes.TrimRight(caCert, "\n")...)
	bundle = append(bundle, '\n')
	return append(bundle, roots...), err
}

// systemRootsPEM returns the trusted roots from the Windows certificate store
// which the shipped certificates do not already cover, together with the
// reasons for anything it could not read.
//
// The ROOT store merges the machine's roots with the current user's, which is
// what the operating system, and so every other program on it, trusts. That
// includes anything the user has added themselves.
func systemRootsPEM() ([]byte, error) {
	shipped, err := certFingerprints(caCert)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	var skipped []error
	err = eachStoreCert("ROOT", func(certContext *windows.CertContext, der []byte) error {
		fingerprint := sha256.Sum256(der)
		if shipped[fingerprint] {
			return nil
		}
		usable, err := usableTLSRoot(certContext, der)
		if err != nil {
			// One certificate Windows will not report on must not cost the
			// trust in every other root, so only it is left out.
			skipped = append(skipped, fmt.Errorf("skipped the certificate %x: %w", fingerprint[:8], err))
			return nil
		}
		if !usable {
			return nil
		}
		return pem.Encode(&out, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	})
	if err != nil {
		skipped = append(skipped, err)
	}
	return out.Bytes(), errors.Join(skipped...)
}

// usableTLSRoot reports whether a certificate from the store belongs in the
// bundle: one Windows allows to authenticate a TLS server, which is currently
// valid, and which Go can read. It returns an error for a certificate it cannot
// judge, which the caller has to leave out rather than trust.
func usableTLSRoot(certContext *windows.CertContext, der []byte) (bool, error) {
	allowed, err := certAllowsServerAuth(certContext)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, nil
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		// A store can hold a certificate Go cannot read, and one which cannot
		// be parsed would make the whole file unusable.
		return false, nil
	}
	now := time.Now()
	return !now.Before(cert.NotBefore) && !now.After(cert.NotAfter), nil
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

var procCertGetEnhancedKeyUsage = windows.NewLazySystemDLL("crypt32.dll").NewProc("CertGetEnhancedKeyUsage")

// certAllowsServerAuth reports whether a certificate's effective Windows EKUs
// allow it to authenticate a TLS server. Windows combines the EKU extension in
// the encoded certificate with an EKU property held only in the store. The
// property has to be checked before exporting the certificate to PEM, which
// cannot preserve it.
func certAllowsServerAuth(certContext *windows.CertContext) (bool, error) {
	var size uint32
	result, _, callErr := procCertGetEnhancedKeyUsage.Call(
		uintptr(unsafe.Pointer(certContext)),
		0,
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if result == 0 {
		return false, fmt.Errorf("could not read certificate purposes: %w", callErr)
	}
	if size < uint32(unsafe.Sizeof(windows.CertEnhKeyUsage{})) {
		return false, fmt.Errorf("could not read certificate purposes: unexpected data size %d", size)
	}

	wordSize := uint32(unsafe.Sizeof(uintptr(0)))
	buffer := make([]uintptr, (size+wordSize-1)/wordSize)
	usage := (*windows.CertEnhKeyUsage)(unsafe.Pointer(&buffer[0]))
	result, _, callErr = procCertGetEnhancedKeyUsage.Call(
		uintptr(unsafe.Pointer(certContext)),
		0,
		uintptr(unsafe.Pointer(usage)),
		uintptr(unsafe.Pointer(&size)),
	)
	if result == 0 {
		return false, fmt.Errorf("could not read certificate purposes: %w", callErr)
	}
	if usage.Length == 0 {
		// CRYPT_E_NOT_FOUND means there is no restriction, while a zero last
		// error means the certificate has explicitly been given no valid uses.
		return errors.Is(callErr, windows.Errno(windows.CRYPT_E_NOT_FOUND)), nil
	}

	for _, identifier := range unsafe.Slice(usage.UsageIdentifiers, usage.Length) {
		switch windows.BytePtrToString(identifier) {
		case "1.3.6.1.5.5.7.3.1", // Server Authentication.
			"1.3.6.1.4.1.311.10.3.3", // Microsoft Server Gated Crypto.
			"2.16.840.1.113730.4.1",  // Netscape Server Gated Crypto.
			"2.5.29.37.0":            // Any Extended Key Usage.
			return true, nil
		}
	}
	return false, nil
}

// eachStoreCert calls fn with the context and encoded bytes of every
// certificate in a Windows system store, as read by every program which
// verifies against it. The context is needed for properties not held in DER.
func eachStoreCert(name string, fn func(certContext *windows.CertContext, der []byte) error) error {
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
		if err := fn(certContext, unsafe.Slice(certContext.EncodedCert, certContext.Length)); err != nil {
			windows.CertFreeCertificateContext(certContext) //nolint:errcheck
			return err
		}
	}
}
