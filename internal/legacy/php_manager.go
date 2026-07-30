package legacy

type phpManager interface {
	// copy writes embedded PHP files to temporary files.
	copy() error

	// binPath returns the path to the temporary PHP binary.
	binPath() string

	// settings returns PHP INI entries (key=value format).
	settings() []string

	// warnings returns anything from copy which is worth reporting, but is not
	// a reason to stop the CLI from running.
	warnings() []string
}

type phpManagerPerOS struct {
	cacheDir string

	// copyWarnings is set by copy, on the platforms which have any.
	copyWarnings []string
}

func newPHPManager(cacheDir string) phpManager {
	return &phpManagerPerOS{cacheDir: cacheDir}
}

func (m *phpManagerPerOS) warnings() []string {
	return m.copyWarnings
}
