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
	return m.writeCAFile()
}

func (m *phpManagerPerOS) binPath() string {
	return filepath.Join(m.cacheDir, "php.exe")
}

// writeCAFile writes the CA bundle, which Windows needs to be given
// explicitly, and which depends on the machine's certificate store.
func (m *phpManagerPerOS) writeCAFile() error {
	bundle, err := caBundle()
	if err != nil {
		// The shipped certificates are still written, so everything except an
		// organization's own certificates keeps working. That is what the CLI
		// trusted before it read the store, and better than running nothing.
		m.copyWarnings = append(m.copyWarnings, err.Error())
	}
	return file.WriteIfChanged(m.caFilePath(), bundle, 0o644)
}

func (m *phpManagerPerOS) caFilePath() string {
	return filepath.Join(m.cacheDir, "cacert.pem")
}

func (m *phpManagerPerOS) settings() []string {
	return []string{
		iniSetting("openssl.cafile", m.caFilePath()),
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
