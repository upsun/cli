package lint

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// First-party config conventions. The native format and directory of any CLI
// come from its config (see Vendor); these constants are the cross-brand
// counterpart that a first-party build also recognizes.
const (
	flavorUpsun        = "upsun"  // project_config_flavor for the Flex format
	firstPartyFlexDir  = ".upsun" // Upsun (Flex) project config directory
	firstPartyFixedDir = ".platform"
	firstPartyFixedApp = ".platform.app.yaml"
)

// Vendor describes the running CLI's configuration conventions, taken from the
// service section of its config (project_config_flavor, project_config_dir,
// app_config_file). It determines the native format and directory names, and
// whether a first-party cross-brand counterpart is also recognized.
type Vendor struct {
	Flavor    string // "upsun" (Flex) or "platform" (Fixed)
	ConfigDir string // e.g. ".upsun", ".platform", ".acme"
	AppFile   string // Fixed per-app config file, e.g. ".platform.app.yaml"
}

// fixedNames is the pair of names that identify a Fixed-style layout.
type fixedNames struct {
	dir string // config directory, e.g. ".platform"
	app string // per-app config file, e.g. ".platform.app.yaml"
}

// candidates returns the Flex directories and Fixed name-sets to look for. The
// native format comes from the vendor config; a first-party build (config dir
// is .upsun or .platform) also recognizes the other first-party format.
func (v Vendor) candidates() (flexDirs []string, fixed []fixedNames) {
	cfgDir := v.ConfigDir
	if cfgDir == "" {
		cfgDir = firstPartyFlexDir
	}
	if v.Flavor == flavorUpsun {
		flexDirs = []string{cfgDir}
		if cfgDir == firstPartyFlexDir { // First-party Upsun: Platform.sh counterpart.
			fixed = []fixedNames{{firstPartyFixedDir, firstPartyFixedApp}}
		}
		return flexDirs, fixed
	}
	app := v.AppFile
	if app == "" {
		app = firstPartyFixedApp
	}
	fixed = []fixedNames{{cfgDir, app}}
	if cfgDir == firstPartyFixedDir { // First-party Platform.sh: Upsun counterpart.
		flexDirs = []string{firstPartyFlexDir}
	}
	return flexDirs, fixed
}

// CheckDir detects the configuration style in dir and lints it, returning the
// detected style. Detection uses the vendor's name conventions and the files
// present: Flex wins when present, else Fixed, else the native format.
func CheckDir(ctx context.Context, dir string, vendor Vendor) (*Result, Style, error) {
	flexDirs, fixedSet := vendor.candidates()
	flexDir, flexOK := detectFlex(dir, flexDirs)
	fixedCfg, fixedOK := detectFixed(dir, fixedSet)

	switch {
	case flexOK:
		content, err := getMergedConfigFiles(os.DirFS(dir), ".", flexDir)
		if err != nil {
			return nil, StyleFlex, err
		}
		result, err := CheckContent(ctx, content)
		if err != nil {
			return nil, StyleFlex, err
		}
		addStrayWarnings(result, dir, flexDirs, fixedSet)
		if fixedOK {
			result.AddWarning("", fmt.Sprintf(
				"both %s and %s configuration found; linting the %s (Flex) configuration",
				flexDir, fixedCfg.dir, flexDir))
		}
		return result, StyleFlex, nil
	case fixedOK:
		result, err := lintFixed(ctx, dir, fixedCfg)
		if err != nil {
			return nil, StyleFixed, err
		}
		addStrayWarnings(result, dir, flexDirs, fixedSet)
		return result, StyleFixed, nil
	default:
		return nil, StyleUnknown, fmt.Errorf(
			"no configuration found in %q (looked for %s)", dir, lookedFor(flexDirs, fixedSet))
	}
}

// detectFlex returns the first candidate Flex directory that contains YAML files.
func detectFlex(root string, dirs []string) (string, bool) {
	for _, d := range dirs {
		if files, err := findFlexConfigFiles(os.DirFS(root), ".", d); err == nil && len(files) > 0 {
			return d, true
		}
	}
	return "", false
}

// detectFixed returns the first candidate Fixed layout anchored at the root: its
// config directory exists, or a per-app config file sits at the top level. The
// anchor is required so a stray nested config file (e.g. a test fixture) does not
// turn an unrelated repository into a project; nested per-app files are still
// collected once a project is confirmed (see loadFixedApplications).
func detectFixed(root string, set []fixedNames) (fixedNames, bool) {
	for _, f := range set {
		if fi, err := os.Stat(filepath.Join(root, f.dir)); err == nil && fi.IsDir() {
			return f, true
		}
		if _, ok := firstExistingYAML(root, "", f.app); ok {
			return f, true
		}
	}
	return fixedNames{}, false
}

// lookedFor builds the hint listing the locations CheckDir searched.
func lookedFor(flexDirs []string, fixed []fixedNames) string {
	parts := make([]string, 0, len(flexDirs)+2*len(fixed))
	for _, d := range flexDirs {
		parts = append(parts, d+"/*.yaml")
	}
	for _, f := range fixed {
		parts = append(parts, f.app, f.dir+"/")
	}
	return strings.Join(parts, ", ")
}

// FindProjectRoot resolves the project root for a path: the nearest ancestor
// directory that contains a .git entry, or the path itself if none is found.
// This lets the command be run from anywhere inside a repository. The nearest
// (not topmost) .git is used so a stray repository higher up the tree (e.g. a
// dotfiles repo in the home directory) cannot hijack the result.
func FindProjectRoot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	for dir := abs; ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		dir = parent
	}
}

// addStrayWarnings warns about nested copies of any config directory the vendor
// knows about. The platform only reads a config directory at the project root,
// so any below it are ignored.
func addStrayWarnings(result *Result, root string, flexDirs []string, fixed []fixedNames) {
	configDirs := map[string]bool{}
	for _, d := range flexDirs {
		configDirs[d] = true
	}
	for _, f := range fixed {
		configDirs[f.dir] = true
	}
	walkProject(root, func(path string, d fs.DirEntry) {
		// A known config directory below the root (not a root-level copy, which
		// detection already handles) is ignored by the platform.
		if d.IsDir() && configDirs[d.Name()] && filepath.Dir(path) != root {
			result.AddWarning(relTo(root, path),
				fmt.Sprintf("this %s directory is not at the project root and will be ignored", d.Name()))
		}
	})
}

// maxFixedAppDepth bounds how deep the walk descends looking for per-app config
// files. It mirrors the legacy CLI's safeguard against slow, over-broad searches.
const maxFixedAppDepth = 5

// walkProject walks root, pruning VCS/dependency directories and limiting depth,
// invoking visit for each remaining entry (files and directories).
func walkProject(root string, visit func(path string, d fs.DirEntry)) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != root {
			switch d.Name() {
			case ".git", ".idea", "node_modules", "vendor", "builds":
				return fs.SkipDir
			}
			if rel, err := filepath.Rel(root, path); err == nil &&
				len(strings.Split(filepath.ToSlash(rel), "/")) >= maxFixedAppDepth {
				return fs.SkipDir
			}
		}
		visit(path, d)
		return nil
	})
}

// findFixedAppFiles returns the paths of per-app config files (appFile, or its
// .yml variant) under dir, descending into subdirectories up to maxFixedAppDepth.
func findFixedAppFiles(dir, appFile string) []string {
	names := yamlVariants(appFile)
	var found []string
	walkProject(dir, func(path string, d fs.DirEntry) {
		if d.IsDir() {
			return
		}
		for _, name := range names {
			if d.Name() == name {
				found = append(found, path)
				break
			}
		}
	})
	return found
}

// firstExistingYAML returns the first existing path among the .yaml/.yml variants
// of base within dir under root, and whether one was found.
func firstExistingYAML(root, dir, base string) (string, bool) {
	for _, name := range yamlVariants(base) {
		p := filepath.Join(root, dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// yamlVariants returns base with both .yaml and .yml extensions, stripping any
// existing .yaml/.yml suffix first.
func yamlVariants(base string) []string {
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	return []string{base + ".yaml", base + ".yml"}
}
