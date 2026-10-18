package platformify

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// skipDirs are not searched for project files.
var skipDirs = []string{"vendor", "node_modules", ".next", ".git"}

// findAllFiles returns the paths of files with the given name under dir.
func findAllFiles(dir, name string) []string {
	var found []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if slices.Contains(skipDirs, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == name {
			found = append(found, p)
		}
		return nil
	})
	return found
}

// findFile returns the path of the file with the given name under dir that is closest to dir, or "".
func findFile(dir, name string) string {
	files := findAllFiles(dir, name)
	if len(files) == 0 {
		return ""
	}
	return slices.MinFunc(files, func(a, b string) int {
		return cmp.Compare(strings.Count(a, string(os.PathSeparator)), strings.Count(b, string(os.PathSeparator)))
	})
}

// fileExists reports whether a file with the given name exists under dir.
func fileExists(dir, name string) bool {
	return findFile(dir, name) != ""
}

// fileContains reports whether the file contains s, ignoring case.
func fileContains(path, s string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(bytes.ToLower(b), []byte(strings.ToLower(s)))
}

// jsonValue returns the value at keyPath in a JSON file, matching keys case-insensitively.
func jsonValue(path string, keyPath ...string) (any, bool) {
	return decodedValue(path, json.Unmarshal, keyPath)
}

// tomlValue returns the value at keyPath in a TOML file, matching keys case-insensitively.
func tomlValue(path string, keyPath ...string) (any, bool) {
	return decodedValue(path, toml.Unmarshal, keyPath)
}

func decodedValue(path string, unmarshal func([]byte, any) error, keyPath []string) (any, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var value any
	if err := unmarshal(b, &value); err != nil {
		return nil, false
	}
	for _, key := range keyPath {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = lookupFold(m, key)
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// lookupFold returns the value of a key in m, matched case-insensitively.
func lookupFold(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}
