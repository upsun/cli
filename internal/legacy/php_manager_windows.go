package legacy

import (
	_ "embed"
	"path/filepath"

	"github.com/upsun/cli/internal/file"
)

//go:embed archives/php_windows.exe
var phpCLI []byte

// copy writes the PHP binary. Its curl is built with Schannel, which verifies
// certificates with Windows (its certificate store, and its distrust and
// revocation lists) unless given a CA file, so none is written.
func (m *phpManagerPerOS) copy() error {
	return file.WriteIfNeeded(m.binPath(), phpCLI, 0o755)
}

func (m *phpManagerPerOS) binPath() string {
	return filepath.Join(m.cacheDir, "php.exe")
}
