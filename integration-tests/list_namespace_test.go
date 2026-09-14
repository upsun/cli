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

// commandNames returns the command names listed in a command listing.
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
// "list <namespace>". Symfony lists the namespace itself when the name is not a
// command, but it builds its own descriptor, which describes lazily-loaded
// commands without resolving them: hidden and disabled commands were listed,
// and the formatting differed from the rest of the CLI.
func TestListNamespace(t *testing.T) {
	f := newCommandFactory(t, "", "")

	viaList, _, err := f.RunCombinedOutput("list", "project")
	require.NoError(t, err)

	// A namespace is not a command, so the CLI reports a failure, as before.
	viaNamespace, viaNamespaceErr, _ := f.RunCombinedOutput("project")
	// Symfony writes this listing to stderr.
	namespaceListing := viaNamespace + viaNamespaceErr

	// The listing comes from the legacy CLI, which does not know the commands
	// implemented in Go: the list command adds those to its own output. So the
	// namespace listing is the list output minus the Go commands.
	goCommands := []string{"project:init"}
	var expected []string
	for _, name := range commandNames(viaList) {
		if !slices.Contains(goCommands, name) {
			expected = append(expected, name)
		}
	}
	assert.Equal(t, expected, commandNames(namespaceListing),
		"naming a namespace must list the same commands as 'list <namespace>'")
	for _, name := range goCommands {
		assert.Contains(t, commandNames(viaList), name,
			"the list command must still add the commands implemented in Go")
	}

	// Asking for help on a namespace lists it too, rather than reporting an
	// ambiguous command name. Unlike the bare namespace, this was asked for
	// explicitly, so it succeeds and writes to stdout.
	viaHelp, _, err := f.RunCombinedOutput("help", "project")
	require.NoError(t, err)
	assert.Equal(t, commandNames(namespaceListing), commandNames(viaHelp),
		"'help <namespace>' must list the same commands as the namespace itself")
	assert.NotContains(t, viaHelp, "is ambiguous")

	// The options of the help command are passed on to the listing. --raw lists
	// the commands unindented and without the headings, so it is checked by
	// content rather than with commandNames().
	viaHelpRaw, _, err := f.RunCombinedOutput("help", "project", "--raw")
	require.NoError(t, err)
	assert.Contains(t, viaHelpRaw, "project:list")
	assert.NotContains(t, viaHelpRaw, "Available commands", "--raw omits the headings")
	assert.NotContains(t, viaHelpRaw, "project:curl", "hidden commands stay hidden")

	viaHelpJSON, _, err := f.RunCombinedOutput("help", "project", "--format=json")
	require.NoError(t, err)
	var described struct {
		Commands map[string]any `json:"commands"`
	}
	require.NoError(t, json.Unmarshal([]byte(viaHelpJSON), &described))
	assert.Contains(t, described.Commands, "project:list")
	assert.NotContains(t, described.Commands, "project:curl", "hidden commands stay hidden")

	// Hidden commands stay hidden: project:curl, and the deprecated
	// project:variable:* commands, are only listed by 'list --all'.
	for _, listing := range []string{namespaceListing, viaHelp} {
		for _, hidden := range []string{"project:curl", "project:variable:get"} {
			assert.NotContains(t, listing, hidden)
		}
	}
	viaListAll, _, err := f.RunCombinedOutput("list", "project", "--all")
	require.NoError(t, err)
	assert.Contains(t, viaListAll, "project:curl", "--all must still show hidden commands")

	// The listing uses the CLI's own descriptor: aliases in parentheses, not
	// Symfony's "[alias|alias]" form.
	assert.Contains(t, namespaceListing, "(projects, pro)")
	assert.NotContains(t, namespaceListing, "[projects|pro]")

	// An abbreviated namespace resolves as well. It used to be reported as
	// ambiguous, because the hidden project:variable:* commands made
	// "project:variable" count as a second namespace matching "proj".
	viaAbbreviation, viaAbbreviationErr, _ := f.RunCombinedOutput("proj")
	assert.Equal(t, commandNames(namespaceListing), commandNames(viaAbbreviation+viaAbbreviationErr))
	assert.NotContains(t, viaAbbreviation+viaAbbreviationErr, "is ambiguous")
}

// TestCompletionHidesHiddenCommands checks that command name completion does not
// suggest hidden commands. Symfony skips them, but it read the hidden state from
// the LazyCommand wrapper, which does not have the one this CLI decides on.
func TestCompletionHidesHiddenCommands(t *testing.T) {
	f := newCommandFactory(t, "", "")

	// The long options are what the completion scripts send.
	suggestions, _, err := f.RunCombinedOutput("_complete", "--no-interaction",
		"--shell=zsh", "--api-version=1", "--current=1", "--input=platform-test", "--input=")
	require.NoError(t, err)

	// Each suggestion is a name and a description, separated by a tab.
	lines := strings.Split(strings.TrimSpace(suggestions), "\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		names = append(names, strings.SplitN(line, "\t", 2)[0])
	}
	require.Greater(t, len(names), 100, "the whole command list should be suggested")

	// Hidden commands: project:curl and its siblings, and the deprecated
	// project:variable:* commands.
	for _, hidden := range []string{"project:curl", "api:curl", "project:variable:get"} {
		assert.NotContains(t, names, hidden, "a hidden command must not be suggested")
	}
	// Ordinary commands are still suggested.
	for _, visible := range []string{"project:list", "environment:list"} {
		assert.Contains(t, names, visible)
	}
}

// TestListNamespaceDisabledCommand checks that a command disabled by
// configuration, as a vendor distribution does, is not listed by any of the
// listing paths.
func TestListNamespaceDisabledCommand(t *testing.T) {
	baseConfig, err := os.ReadFile("config.yaml")
	require.NoError(t, err)

	// Disable project:create, the way a vendor configuration does.
	var cnf map[string]any
	require.NoError(t, yaml.Unmarshal(baseConfig, &cnf))
	application, ok := cnf["application"].(map[string]any)
	require.True(t, ok, "the test config must have an application section")
	application["disabled_commands"] = []string{"project:create"}

	out, err := yaml.Marshal(cnf)
	require.NoError(t, err)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, out, 0o600))

	f := newCommandFactory(t, "", "")
	// A later CLI_CONFIG_FILE wins over the one testEnv sets.
	f.extraEnv = []string{"CLI_CONFIG_FILE=" + configPath}

	viaList, _, err := f.RunCombinedOutput("list", "project")
	require.NoError(t, err)
	assert.NotContains(t, viaList, "project:create")

	viaNamespace, viaNamespaceErr, _ := f.RunCombinedOutput("project")
	assert.NotContains(t, viaNamespace+viaNamespaceErr, "project:create",
		"a disabled command must not be listed for its namespace")

	viaHelp, _, err := f.RunCombinedOutput("help", "project")
	require.NoError(t, err)
	assert.NotContains(t, viaHelp, "project:create",
		"a disabled command must not be listed by 'help <namespace>'")

	// It is not listed even with --all: it cannot be run at all.
	viaListAll, _, err := f.RunCombinedOutput("list", "project", "--all")
	require.NoError(t, err)
	assert.NotContains(t, viaListAll, "project:create")

	// Sanity check: the same listing without the override does show it.
	f.extraEnv = nil
	plain, _, err := f.RunCombinedOutput("list", "project")
	require.NoError(t, err)
	assert.Contains(t, plain, "project:create")
	assert.True(t, strings.Contains(plain, "project:list"))
}
