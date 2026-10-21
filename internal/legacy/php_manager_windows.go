package legacy

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/upsun/cli/internal/file"
)

//go:embed archives/php_windows.exe
var phpCLI []byte

// copy writes the PHP binary. Its curl is built with Schannel, which verifies
// certificates with Windows (its certificate store, and its distrust and
// revocation lists) unless given a CA file (see settings).
func (m *phpManagerPerOS) copy() error {
	return file.WriteIfNeeded(m.binPath(), phpCLI, 0o755)
}

func (m *phpManagerPerOS) binPath() string {
	return filepath.Join(m.cacheDir, "php.exe")
}

// settings passes SSL_CERT_FILE to PHP as openssl.cafile, which PHP gives to
// curl. Schannel then verifies certificates against that file instead of the
// Windows certificate store, as Go does for the rest of the CLI.
//
// PHP reads the path from the environment itself, so it needs no escaping.
func (m *phpManagerPerOS) settings() []string {
	if os.Getenv("SSL_CERT_FILE") != "" {
		return []string{`openssl.cafile="${SSL_CERT_FILE}"`}
	}
	return nil
}
