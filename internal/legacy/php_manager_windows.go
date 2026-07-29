package legacy

import (
	_ "embed"
	"path/filepath"
	"strings"

	"github.com/upsun/cli/internal/file"
)

//go:embed archives/php_windows.exe
var phpCLI []byte

//go:embed archives/cacert.pem
var caCert []byte

func (m *phpManagerPerOS) copy() error {
	if err := file.WriteIfNeeded(m.binPath(), phpCLI, 0o755); err != nil {
		return err
	}
	// Write cacert.pem for OpenSSL CA bundle (Windows needs this explicitly).
	return file.WriteIfNeeded(filepath.Join(m.cacheDir, "cacert.pem"), caCert, 0o644)
}

func (m *phpManagerPerOS) binPath() string {
	return filepath.Join(m.cacheDir, "php.exe")
}

func (m *phpManagerPerOS) settings() []string {
	return []string{
		iniSetting("openssl.cafile", filepath.Join(m.cacheDir, "cacert.pem")),
	}
}

// iniSetting formats a PHP setting for the -d option.
//
// PHP reads ini values as expressions, in which characters such as "~" are
// operators, so a value has to be quoted. Windows short paths contain them,
// for example C:\Users\RUNNER~1\AppData. Inside quotes a backslash escapes the
// next character, so the backslashes have to be doubled.
func iniSetting(key, value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return key + `="` + escaped + `"`
}
