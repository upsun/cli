package lint

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Style identifies which configuration style a project uses.
type Style int

const (
	StyleUnknown Style = iota
	// StyleFlex is the Upsun unified configuration (.upsun/*.yaml).
	StyleFlex
	// StyleFixed is the legacy Platform.sh configuration (.platform.app.yaml, .platform/*.yaml).
	StyleFixed
)

func (s Style) String() string {
	switch s {
	case StyleFlex:
		return "flex"
	case StyleFixed:
		return "fixed"
	default:
		return "unknown"
	}
}

// CheckDir detects the configuration style in dir and lints it, returning the
// detected style. Detection is based purely on the directory layout.
func CheckDir(ctx context.Context, dir string) (*Result, Style, error) {
	flex := hasFlexConfig(dir)
	fixed := hasFixedConfig(dir)

	switch {
	case flex:
		content, err := loadFlex(dir)
		if err != nil {
			return nil, StyleFlex, err
		}
		result, err := CheckContent(ctx, content)
		if err != nil {
			return nil, StyleFlex, err
		}
		if fixed {
			result.AddWarning("", "both .upsun and .platform configuration found; "+
				"linting the .upsun (Flex) configuration")
		}
		return result, StyleFlex, nil
	case fixed:
		result, err := lintFixed(ctx, dir)
		return result, StyleFixed, err
	default:
		return nil, StyleUnknown, fmt.Errorf(
			"no Upsun configuration found in %q (looked for .upsun/*.yaml and .platform[.app].yaml)", dir)
	}
}

// loadFlex merges the .upsun/*.yaml files in dir into a single YAML document.
func loadFlex(dir string) (string, error) {
	return getMergedConfigFiles(os.DirFS(dir), ".")
}

// hasFlexConfig reports whether dir contains .upsun/*.yaml configuration files.
func hasFlexConfig(dir string) bool {
	files, err := findUpsunConfigFiles(os.DirFS(dir), ".")
	return err == nil && len(files) > 0
}

// hasFixedConfig reports whether dir contains legacy Platform.sh configuration.
func hasFixedConfig(dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, ".platform")); err == nil && fi.IsDir() {
		return true
	}
	return len(findFixedAppFiles(dir)) > 0
}

// findFixedAppFiles returns the paths of all .platform.app.yaml files under dir.
func findFixedAppFiles(dir string) []string {
	var found []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				if path != dir {
					return fs.SkipDir
				}
			}
			return nil
		}
		if d.Name() == fixedAppConfig {
			found = append(found, path)
		}
		return nil
	})
	return found
}
