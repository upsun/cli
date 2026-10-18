package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// style identifies which configuration style a project uses.
type style int

const (
	styleUnknown style = iota
	// styleFlex is the Upsun unified configuration (.upsun/*.yaml).
	styleFlex
	// styleFixed is the legacy Platform.sh configuration (.platform.app.yaml, .platform/*.yaml).
	styleFixed
)

// Configuration locations, relative to the project root.
const (
	flexDir      = ".upsun"             // Flex: .upsun/*.yaml
	fixedDir     = ".platform"          // Fixed: applications, routes and services
	fixedAppFile = ".platform.app.yaml" // Fixed: per-app config file
)

// CheckDir detects the configuration style in dir and lints it: Flex
// (.upsun/*.yaml) when present, else Fixed (.platform/ or .platform.app.yaml).
// Configuration problems are reported in the Result; the error is for
// configuration that cannot be found, read or parsed.
func CheckDir(dir string) (*Result, error) {
	result, _, err := checkDir(dir)
	return result, err
}

// checkDir is CheckDir, also returning the detected style.
func checkDir(dir string) (*Result, style, error) {
	switch {
	case isFlex(dir):
		root, err := os.OpenRoot(dir)
		if err != nil {
			return nil, styleFlex, err
		}
		defer root.Close()
		// The root keeps !include tags from following symbolic links out of the project.
		content, sources, err := getMergedConfigFiles(root.FS(), ".", flexDir)
		var result *Result
		var srcErr *sourceError
		switch {
		case errors.As(err, &srcErr):
			result = &Result{Errors: []Issue{srcErr.issue()}}
		case err != nil:
			return nil, styleFlex, err
		default:
			if result, err = CheckContent(content); err != nil {
				return nil, styleFlex, err
			}
			sources.locate(result)
		}
		addStrayWarnings(result, dir)
		if isFixed(dir) {
			result.AddWarning("", fmt.Sprintf(
				"both %s and %s configuration found; linting the %s (Flex) configuration",
				flexDir, fixedDir, flexDir))
		}
		return result, styleFlex, nil
	case isFixed(dir):
		result, err := lintFixed(dir)
		if err != nil {
			return nil, styleFixed, err
		}
		addStrayWarnings(result, dir)
		return result, styleFixed, nil
	default:
		return nil, styleUnknown, fmt.Errorf("no configuration found in %q (looked for %s/*.yaml, %s, %s/)",
			dir, flexDir, fixedAppFile, fixedDir)
	}
}

// isFlex reports whether the Flex config directory under root contains YAML files.
func isFlex(root string) bool {
	files, err := findFlexConfigFiles(os.DirFS(root), ".", flexDir)
	return err == nil && len(files) > 0
}

// isFixed reports whether root has a Fixed layout: the config directory exists,
// or a per-app config file sits at the top level. The anchor is required so a
// stray nested config file (e.g. a test fixture) does not turn an unrelated
// repository into a project; nested per-app files are still collected once a
// project is confirmed (see loadApplications).
func isFixed(root string) bool {
	if fi, err := os.Stat(filepath.Join(root, fixedDir)); err == nil && fi.IsDir() {
		return true
	}
	_, ok := firstExistingYAML(root, "", fixedAppFile)
	return ok
}

// addStrayWarnings warns about nested config directories. The platform only
// reads a config directory at the project root, so any below it are ignored.
func addStrayWarnings(result *Result, root string) {
	walkProject(root, func(path string, d fs.DirEntry) {
		// A config directory below the root (not a root-level copy, which
		// detection already handles) is ignored by the platform.
		if d.IsDir() && (d.Name() == flexDir || d.Name() == fixedDir) && filepath.Dir(path) != root {
			result.Warnings = append(result.Warnings, Issue{
				File:    filepath.ToSlash(relTo(root, path)),
				Message: fmt.Sprintf("this %s directory is not at the project root and will be ignored", d.Name()),
			})
		}
	})
}

// maxFixedAppDepth bounds how deep the walk descends looking for per-app config
// files. It mirrors the legacy CLI's safeguard against slow, over-broad searches.
const maxFixedAppDepth = 5

// walkProject walks root, pruning VCS/dependency directories, separate Git
// checkouts, and limiting depth, invoking visit for each remaining entry (files
// and directories).
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
			if isSeparateCheckout(path) {
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

// isSeparateCheckout reports whether dir is a Git checkout other than a
// submodule: a separate clone (a .git directory) or a linked worktree (a .git
// file pointing into another repository's "worktrees" directory). These are not
// part of the project. Submodules also have a .git file, pointing into
// "modules", and are kept since they are deployed with the project.
func isSeparateCheckout(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	fi, err := os.Lstat(gitPath)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		return true
	}
	b, err := os.ReadFile(gitPath)
	if err != nil {
		return false
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	return ok && strings.Contains(filepath.ToSlash(strings.TrimSpace(gitDir)), "/worktrees/")
}
