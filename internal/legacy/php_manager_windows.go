package legacy

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

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
func (m *phpManagerPerOS) settings() []string {
	if f := os.Getenv("SSL_CERT_FILE"); f != "" {
		return []string{iniSetting("openssl.cafile", f)}
	}
	return nil
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
