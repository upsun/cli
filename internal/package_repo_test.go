package internal

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Outputs captured from the package managers, for a package installed from a
// repository, and from a downloaded file.
const (
	aptPolicyRepo = `upsun-cli:
  Installed: 5.17.0
  Candidate: 5.17.0
  Version table:
 *** 5.17.0 500
        500 https://repositories.upsun.com/debian stable/main amd64 Packages
        100 /var/lib/dpkg/status
     5.16.0 500
        500 https://repositories.upsun.com/debian stable/main amd64 Packages
`
	aptPolicyFile = `upsun-cli:
  Installed: 5.17.0
  Candidate: 5.17.0
  Version table:
 *** 5.17.0 100
        100 /var/lib/dpkg/status
`
	apkPolicyRepo = `upsun-cli policy:
  5.17.0:
    lib/apk/db/installed
    https://repositories.upsun.com/alpine
`
	apkPolicyFile = `upsun-cli policy:
  5.17.0:
    lib/apk/db/installed
`
	dnfRepolist = `repo id               repo name
fedora                Fedora 45 - x86_64
updates               Fedora 45 - x86_64 - Updates
upsun                 Upsun CLI
`
	yumInfoRepo = `Installed Packages
Name        : upsun-cli
Version     : 5.17.0
Repo        : installed
From repo   : upsun
`
	yumInfoFile = `Installed Packages
Name        : upsun-cli
Version     : 5.17.0
Repo        : installed
`
	yumRepolist = `repo id                        repo name                                  status
amzn2-core/2/x86_64            Amazon Linux 2 core repository             44236
upsun/x86_64                   Upsun CLI                                     26
`
)

func TestInRepository(t *testing.T) {
	cases := []struct {
		name    string
		method  InstallMethod
		outputs map[string]string // command line -> output; other commands fail
		want    bool
	}{
		{"apt repository", InstallApt, map[string]string{"apt-cache policy upsun-cli": aptPolicyRepo}, true},
		{"apt file", InstallApt, map[string]string{"apt-cache policy upsun-cli": aptPolicyFile}, false},
		{"apt query fails", InstallApt, nil, false},
		{"apk repository", InstallApk, map[string]string{"apk --no-network policy upsun-cli": apkPolicyRepo}, true},
		{"apk file", InstallApk, map[string]string{"apk --no-network policy upsun-cli": apkPolicyFile}, false},
		{"dnf repository", InstallDnf, map[string]string{
			"dnf -C -q repoquery --installed --qf %{from_repo}\n upsun-cli": "upsun\n\n",
			"dnf -C -q repolist --enabled":                                  dnfRepolist,
		}, true},
		{"dnf repository disabled", InstallDnf, map[string]string{
			"dnf -C -q repoquery --installed --qf %{from_repo}\n upsun-cli": "upsun\n",
			"dnf -C -q repolist --enabled":                                  strings.ReplaceAll(dnfRepolist, "upsun ", "other "),
		}, false},
		{"dnf command line", InstallDnf, map[string]string{
			"dnf -C -q repoquery --installed --qf %{from_repo}\n upsun-cli": "@commandline\n",
			"dnf -C -q repolist --enabled":                                  dnfRepolist,
		}, false},
		{"dnf unknown", InstallDnf, map[string]string{
			"dnf -C -q repoquery --installed --qf %{from_repo}\n upsun-cli": "<unknown>\n",
			"dnf -C -q repolist --enabled":                                  dnfRepolist,
		}, false},
		{"dnf4 rpm -i", InstallDnf, map[string]string{
			"dnf -C -q repoquery --installed --qf %{from_repo}\n upsun-cli": "\n\n",
			"dnf -C -q repolist --enabled":                                  "repo id   repo name\n",
		}, false},
		{"yum repository", InstallYum, map[string]string{
			"yum -C -q info installed upsun-cli": yumInfoRepo,
			"yum -C -q repolist enabled":         yumRepolist,
		}, true},
		{"yum file", InstallYum, map[string]string{
			"yum -C -q info installed upsun-cli": yumInfoFile,
			"yum -C -q repolist enabled":         yumRepolist,
		}, false},
		{"yum repolist fails", InstallYum, map[string]string{
			"yum -C -q info installed upsun-cli": yumInfoRepo,
		}, false},
		{"unknown method", InstallPackage, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			output := func(name string, args ...string) (string, error) {
				if out, ok := c.outputs[name+" "+strings.Join(args, " ")]; ok {
					return out, nil
				}
				return "", errors.New("exit status 1")
			}
			assert.Equal(t, c.want, inRepository(c.method, "upsun-cli", output))
		})
	}
}

func TestRepoEnabled(t *testing.T) {
	assert.True(t, repoEnabled("upsun", dnfRepolist))
	assert.True(t, repoEnabled("upsun", yumRepolist))
	assert.True(t, repoEnabled("amzn2-core", yumRepolist))
	assert.True(t, repoEnabled("upsun", "!upsun/x86_64   Upsun CLI   26\n"))
	assert.False(t, repoEnabled("repo", dnfRepolist))
	assert.False(t, repoEnabled("", dnfRepolist))
	assert.False(t, repoEnabled("other", dnfRepolist))
}

func TestAptPolicyHasRepository(t *testing.T) {
	assert.True(t, aptPolicyHasRepository(aptPolicyRepo))
	assert.False(t, aptPolicyHasRepository(aptPolicyFile))
	assert.False(t, aptPolicyHasRepository(""))
	// A version that is a number, without a repository.
	assert.False(t, aptPolicyHasRepository(
		"x:\n  Version table:\n *** 1 100\n        100 /var/lib/dpkg/status\n     2 500\n"))
}
