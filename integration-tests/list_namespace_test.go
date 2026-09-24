package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var commandNamePattern = regexp.MustCompile(`(?m)^\s{2}(project:[a-z0-9:-]+)`)

// commandNames returns the project:* command names in a command listing.
func commandNames(listing string) []string {
	matches := commandNamePattern.FindAllStringSubmatch(listing, -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1])
	}
	sort.Strings(names)
	return names
}

// TestListNamespace checks that naming a namespace lists the same commands as
// "list <namespace>", without hidden commands.
func TestListNamespace(t *testing.T) {
	f := newCommandFactory(t, "", "")

	viaList, _, err := f.RunCombinedOutput("list", "project")
	require.NoError(t, err)
	// The Go list command adds its own commands, which the legacy CLI does not know.
	expected := slices.DeleteFunc(commandNames(viaList), func(name string) bool { return name == "project:init" })
	require.Contains(t, expected, "project:list")

	cases := []struct {
		args     []string
		wantFail bool // A bare namespace is not a command: it fails and lists on stderr.
	}{
		{args: []string{"project"}, wantFail: true},
		{args: []string{"project", "--help"}},
		{args: []string{"project", "-h"}},
		{args: []string{"help", "project"}},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			stdOut, stdErr, err := f.RunCombinedOutput(c.args...)
			listing := stdOut
			if c.wantFail {
				assert.Error(t, err)
				assert.Empty(t, stdOut)
				listing = stdErr
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, expected, commandNames(listing))
			assert.NotContains(t, listing, "project:curl")
			assert.NotContains(t, listing, "project:variable:get")
			// The CLI's own descriptor, not Symfony's "[projects|pro]".
			assert.Contains(t, listing, "(projects, pro)")
		})
	}

	// The help command's options are passed on to the listing.
	viaHelpRaw, _, err := f.RunCombinedOutput("help", "project", "--raw")
	require.NoError(t, err)
	assert.Contains(t, viaHelpRaw, "project:list")
	assert.NotContains(t, viaHelpRaw, "Available commands")

	viaHelpJSON, _, err := f.RunCombinedOutput("help", "project", "--format=json")
	require.NoError(t, err)
	var described struct {
		Commands map[string]any `json:"commands"`
	}
	require.NoError(t, json.Unmarshal([]byte(viaHelpJSON), &described))
	assert.Contains(t, described.Commands, "project:list")
	assert.NotContains(t, described.Commands, "project:curl")

	// Namespaces of hidden commands still exist, for --all and for suggestions.
	viaListAll, _, err := f.RunCombinedOutput("list", "api", "--all")
	require.NoError(t, err)
	assert.Contains(t, viaListAll, "api:curl")

	_, typoErr, err := f.RunCombinedOutput("api:crl")
	assert.Error(t, err)
	assert.Contains(t, typoErr, `Command "api:crl" is not defined.`)
}

// TestCompletionHidesHiddenCommands checks that command name completion does
// not suggest hidden or disabled commands.
func TestCompletionHidesHiddenCommands(t *testing.T) {
	f := newCommandFactory(t, "", "")
	f.extraEnv = []string{"CLI_CONFIG_FILE=" + configDisabling(t, "project:create")}

	// The long options are what the completion scripts send.
	suggestions, _, err := f.RunCombinedOutput("_complete", "--no-interaction",
		"--shell=zsh", "--api-version=1", "--current=1", "--input=platform-test", "--input=")
	require.NoError(t, err)

	// Each suggestion is a name and a description, separated by a tab.
	var names []string
	for line := range strings.Lines(strings.TrimSpace(suggestions)) {
		names = append(names, strings.SplitN(line, "\t", 2)[0])
	}
	require.Greater(t, len(names), 100)

	for _, hidden := range []string{"project:curl", "api:curl", "project:variable:get", "project:create"} {
		assert.NotContains(t, names, hidden)
	}
	for _, visible := range []string{"project:list", "environment:list"} {
		assert.Contains(t, names, visible)
	}
}

// TestListNamespaceDisabledCommand checks that a command disabled by
// configuration, as a vendor distribution does, is not listed.
func TestListNamespaceDisabledCommand(t *testing.T) {
	f := newCommandFactory(t, "", "")

	plain, _, err := f.RunCombinedOutput("list", "project")
	require.NoError(t, err)
	require.Contains(t, plain, "project:create")

	f.extraEnv = []string{"CLI_CONFIG_FILE=" + configDisabling(t, "project:create")}
	for _, args := range [][]string{{"project"}, {"help", "project"}, {"list", "project", "--all"}} {
		stdOut, stdErr, _ := f.RunCombinedOutput(args...)
		assert.Contains(t, stdOut+stdErr, "project:list", args)
		assert.NotContains(t, stdOut+stdErr, "project:create", args)
	}
}

// configDisabling writes a copy of the test config that disables the given
// commands, for CLI_CONFIG_FILE: a later value wins over the one testEnv sets.
func configDisabling(t *testing.T, commands ...string) string {
	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)
	var cnf map[string]any
	require.NoError(t, yaml.Unmarshal(baseConfig, &cnf))
	application, ok := cnf["application"].(map[string]any)
	require.True(t, ok)
	application["disabled_commands"] = commands

	out, err := yaml.Marshal(cnf)
	require.NoError(t, err)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, out, 0o600))

	return configPath
}
