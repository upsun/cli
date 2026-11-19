package internal

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// packageInRepository asks the package manager whether an enabled repository
// provides the installed package, so that the package manager can upgrade it.
func packageInRepository(m InstallMethod, pkg string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return inRepository(m, pkg, func(name string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		out, err := cmd.Output()
		return string(out), err
	})
}

// inRepository runs the package manager's queries with output, which returns a command's standard output.
func inRepository(m InstallMethod, pkg string, output func(name string, args ...string) (string, error)) bool {
	switch m {
	case InstallApt:
		out, err := output("apt-cache", "policy", pkg)
		return err == nil && aptPolicyHasRepository(out)
	case InstallApk:
		out, err := output("apk", "--no-network", "policy", pkg)
		return err == nil && apkPolicyHasRepository(out)
	case InstallDnf:
		repo, err := output("dnf", "-C", "-q", "repoquery", "--installed", "--qf", "%{from_repo}\n", pkg)
		if err != nil {
			return false
		}
		repos, err := output("dnf", "-C", "-q", "repolist", "--enabled")
		return err == nil && repoEnabled(strings.TrimSpace(repo), repos)
	case InstallYum:
		info, err := output("yum", "-C", "-q", "info", "installed", pkg)
		if err != nil {
			return false
		}
		repos, err := output("yum", "-C", "-q", "repolist", "enabled")
		return err == nil && repoEnabled(yumFromRepo(info), repos)
	}
	return false
}

// aptSource matches a line of the version table of "apt-cache policy" that
// names a source of the version, after its priority.
var aptSource = regexp.MustCompile(`^\s+-?\d+\s+(\S+)`)

// aptPolicyHasRepository reports whether "apt-cache policy" lists a source
// other than the dpkg status file, i.e. a repository.
func aptPolicyHasRepository(out string) bool {
	table := false
	for line := range strings.Lines(out) {
		if strings.TrimSpace(line) == "Version table:" {
			table = true
			continue
		}
		if !table {
			continue
		}
		m := aptSource.FindStringSubmatch(line)
		if len(m) == 2 && m[1] != "/var/lib/dpkg/status" && !isNumber(m[1]) {
			return true
		}
	}
	return false
}

func isNumber(s string) bool {
	return strings.Trim(s, "0123456789") == ""
}

// apkPolicyHasRepository reports whether "apk policy" lists a source other
// than the installed database, i.e. a repository.
func apkPolicyHasRepository(out string) bool {
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") || line == "lib/apk/db/installed" {
			continue
		}
		return true
	}
	return false
}

// yumFromRepo returns the "From repo" field of "yum info installed".
func yumFromRepo(info string) string {
	for line := range strings.Lines(info) {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(k) == "From repo" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// repoEnabled reports whether the repository ID is in the output of "dnf
// repolist" or "yum repolist". yum may prefix an ID with "!" or "*" and
// suffix it with the release and architecture, e.g. "upsun/x86_64".
func repoEnabled(id, repolist string) bool {
	if id == "" {
		return false
	}
	for line := range strings.Lines(repolist) {
		// The header ("repo id  repo name") has no ID.
		if strings.HasPrefix(line, "repo id") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(fields[0], "!*"), "/")
		if name == id {
			return true
		}
	}
	return false
}
