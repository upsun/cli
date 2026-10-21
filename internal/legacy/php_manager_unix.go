//go:build darwin || linux

package legacy

import (
	"path/filepath"

	"github.com/upsun/cli/internal/file"
)

func (m *phpManagerPerOS) copy() error {
	return file.WriteIfNeeded(m.binPath(), phpCLI, 0o755)
}

func (m *phpManagerPerOS) binPath() string {
	return filepath.Join(m.cacheDir, "php")
}

// settings returns no settings: the legacy CLI reads SSL_CERT_FILE itself, through Composer\CaBundle.
func (m *phpManagerPerOS) settings() []string {
	return nil
}
