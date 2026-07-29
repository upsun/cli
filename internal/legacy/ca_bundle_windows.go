package legacy

import (
	"bytes"
	"encoding/pem"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// caBundle returns the certificates the legacy CLI should trust.
//
// The shipped certificates alone are not enough: an organization which inspects
// TLS traffic installs its own root certificate in the Windows store, and every
// other program on the machine then trusts it. curl in the embedded PHP could
// read that store, but only while no CA file is set, and a CA file has to be
// set because the openssl extension cannot read the store at all. So the roots
// from the store are added to the shipped ones.
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

// systemRootsPEM returns the trusted root certificates from the Windows store,
// as read by every program which verifies against it, including Go itself.
func systemRootsPEM() ([]byte, error) {
	store, err := windows.CertOpenSystemStore(0, windows.StringToUTF16Ptr("ROOT"))
	if err != nil {
		return nil, fmt.Errorf("could not open the certificate store: %w", err)
	}
	defer windows.CertCloseStore(store, 0) //nolint:errcheck

	var out bytes.Buffer
	var certContext *windows.CertContext
	for {
		certContext, err = windows.CertEnumCertificatesInStore(store, certContext)
		if certContext == nil {
			if err != nil && !errors.Is(err, windows.Errno(windows.CRYPT_E_NOT_FOUND)) {
				return nil, fmt.Errorf("could not read the certificate store: %w", err)
			}
			return out.Bytes(), nil
		}
		// The context belongs to the store, so the bytes are copied out.
		encoded := unsafe.Slice(certContext.EncodedCert, certContext.Length)
		if err := pem.Encode(&out, &pem.Block{Type: "CERTIFICATE", Bytes: encoded}); err != nil {
			return nil, err
		}
	}
}
