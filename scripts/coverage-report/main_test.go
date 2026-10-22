package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCommands() []command {
	envList := command{Name: "environment:list", Aliases: []string{"environments", "env"}}
	envList.Definition.Options = map[string]option{
		"pipe":  {Name: "--pipe"},
		"type":  {Name: "--type", AcceptValue: true},
		"sort":  {Name: "--sort", Shortcut: "-s", AcceptValue: true},
		"yes":   {Name: "--yes", Shortcut: "-y"},
		"quiet": {Name: "--quiet", Shortcut: "-q"},
	}
	return []command{
		envList,
		{Name: "environment:info"},
		{Name: "project:list", Aliases: []string{"projects"}, HiddenAliases: []string{"pro"}},
		{Name: "project:info"},
		{Name: "project:internal", Hidden: true},
	}
}

func TestResolve(t *testing.T) {
	cmds := testCommands()
	cases := []struct {
		name     string
		expected string
	}{
		{name: "environment:list", expected: "environment:list"},
		{name: "env", expected: "environment:list"},
		{name: "pro", expected: "project:list"},
		{name: "e:l", expected: "environment:list"},
		{name: "env:i", expected: "environment:info"},
		{name: "p:inf", expected: "project:info"},
		{name: "P:INF", expected: "project:info"},
		// Hidden commands are not resolved, but count toward ambiguity.
		{name: "p:int", expected: ""},
		{name: "p:in", expected: ""},
		// Prefix-only matches count toward ambiguity.
		{name: "e", expected: ""},
		{name: "en:", expected: ""},
		{name: "foo", expected: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			i := resolve(cmds, c.name)
			if c.expected == "" {
				assert.Equal(t, -1, i)
				return
			}
			require.NotEqual(t, -1, i)
			assert.Equal(t, c.expected, cmds[i].Name)
		})
	}
}

func TestUsedOptions(t *testing.T) {
	cmd := testCommands()[0]
	cases := []struct {
		args     []string
		expected []string
	}{
		{args: []string{"--pipe", "--type=dev"}, expected: []string{"pipe", "type"}},
		{args: []string{"--type", "--pipe"}, expected: []string{"type", "pipe"}},
		{args: []string{"-yq"}, expected: []string{"yes", "quiet"}},
		{args: []string{"-ysq"}, expected: []string{"yes", "sort"}},
		{args: []string{"-x"}, expected: nil},
		{args: []string{"arg", "-", "--", "--pipe"}, expected: nil},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			assert.Equal(t, c.expected, usedOptions(&cmd, c.args))
		})
	}
}

func TestAnalyze(t *testing.T) {
	cmds := testCommands()
	a := analyze(cmds, [][]string{
		{"env", "--pipe"},
		{"-q", "e:l", "-s", "name"},
		{"--version"},
		{"foo"},
	})
	assert.Equal(t, 2, a.usage[0].runs)
	assert.Equal(t, map[string]bool{"pipe": true, "sort": true}, a.usage[0].options)
	assert.Equal(t, map[string]int{"foo": 1}, a.unresolved)

	var b strings.Builder
	writeCommandCoverage(&b, a)
	assert.Contains(t, b.String(), "### Commands: 1 of 4 run\n")
	assert.Contains(t, b.String(), "### Options: 2 of 3 used\n")
	assert.Contains(t, b.String(), "| `environment:list` | `--type` |\n")
}

func TestParseProfile(t *testing.T) {
	profile := `mode: set
github.com/upsun/cli/commands/list.go:10.2,12.3 2 1
github.com/upsun/cli/commands/list.go:13.2,14.3 3 0
github.com/upsun/cli/internal/api/client.go:1.1,2.2 4 0
github.com/upsun/cli/pkg/mockapi/api.go:1.1,2.2 5 1
`
	pkgs, err := parseProfile(strings.NewReader(profile))
	require.NoError(t, err)
	assert.Equal(t, []pkgCoverage{
		{name: "commands", covered: 2, total: 5},
		{name: "internal/api", covered: 0, total: 4},
	}, pkgs)
}
