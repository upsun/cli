package internal

import (
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// packageRepoFiles lists the files (as glob patterns) that configure each package manager's repositories.
var packageRepoFiles = map[InstallMethod][]string{
	InstallApt: {"/etc/apt/sources.list", "/etc/apt/sources.list.d/*.list", "/etc/apt/sources.list.d/*.sources"},
	InstallDnf: {"/etc/yum.repos.d/*.repo"},
	InstallYum: {"/etc/yum.repos.d/*.repo"},
	InstallApk: {"/etc/apk/repositories", "/etc/apk/repositories.d/*.list"},
}

// dnfOverrideFiles lists the files (as glob patterns) that override repository settings in DNF 5.
var dnfOverrideFiles = []string{"/usr/share/dnf5/repos.override.d/*.repo", "/etc/dnf/repos.override.d/*.repo"}

// packageRepositoryInUse reports whether a package manager's configuration enables the repository.
func packageRepositoryInUse(m InstallMethod, repo string) bool {
	var overrides []string
	if m == InstallDnf {
		overrides = dnfOverrideFiles
	}
	return anyFileEnablesRepo(packageRepoFiles[m], overrides, repo)
}

// anyFileEnablesRepo reports whether a file matching one of the glob patterns
// enables the repository, and no file matching the override patterns disables it.
func anyFileEnablesRepo(patterns, overridePatterns []string, repo string) bool {
	overrides := readOverrides(overridePatterns)
	for _, pattern := range patterns {
		paths, _ := filepath.Glob(pattern)
		for _, path := range paths {
			b, err := os.ReadFile(path)
			if err != nil || !enablesRepo(b, repo) {
				continue
			}
			if len(overrides) == 0 {
				return true
			}
			for _, id := range repoSections(b, repo) {
				if !disabledByOverride(id, overrides) {
					return true
				}
			}
		}
	}
	return false
}

// readOverrides reads the files matching the glob patterns, ordered by file name as DNF applies them.
// A file masks those of the same name that match an earlier pattern.
func readOverrides(patterns []string) [][]byte {
	paths := map[string]string{}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			paths[filepath.Base(path)] = path
		}
	}
	var contents [][]byte
	for _, name := range slices.Sorted(maps.Keys(paths)) {
		if b, err := os.ReadFile(paths[name]); err == nil {
			contents = append(contents, b)
		}
	}
	return contents
}

// sectionName returns the name of an INI section header line, e.g. "[name]".
func sectionName(line string) (string, bool) {
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return strings.TrimSpace(line[1 : len(line)-1]), true
	}
	return "", false
}

// repoSections returns the names of the sections of a yum repo file that mention the repository.
func repoSections(content []byte, repo string) []string {
	var ids []string
	section := ""
	for line := range strings.Lines(string(content)) {
		line = strings.TrimSpace(line)
		if name, ok := sectionName(line); ok {
			section = name
		} else if section != "" && !strings.HasPrefix(line, "#") && strings.Contains(line, repo) {
			ids = append(ids, section)
			section = ""
		}
	}
	return ids
}

// enabledSetting matches a line that enables or disables a repository.
var enabledSetting = regexp.MustCompile(`(?i)^enabled\s*[=:]`)

// disabledByOverride reports whether the override files disable a repository.
// Their section names may be glob patterns, and the last setting applies.
func disabledByOverride(id string, overrides [][]byte) bool {
	disabled := false
	for _, content := range overrides {
		applies := false
		for line := range strings.Lines(string(content)) {
			line = strings.TrimSpace(line)
			if name, ok := sectionName(line); ok {
				applies, _ = path.Match(name, id)
			} else if applies && enabledSetting.MatchString(line) {
				disabled = repoDisabled.MatchString(line)
			}
		}
	}
	return disabled
}

// repoDisabled matches a line that disables a repository, in a yum repo file or a deb822 sources file.
var repoDisabled = regexp.MustCompile(`(?i)^enabled\s*[=:]\s*(0|no|false|off|disable|without)$`)

// enablesRepo reports whether a repository configuration file mentions the
// repository outside comments. A file that disables any repository does not
// count, as it is not parsed to find out which one.
func enablesRepo(content []byte, repo string) bool {
	found := false
	for line := range strings.Lines(string(content)) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#"):
		case repoDisabled.MatchString(line):
			return false
		case strings.Contains(line, repo):
			found = true
		}
	}
	return found
}
