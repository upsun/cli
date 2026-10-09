package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompletionScript checks that the generated completion scripts request
// suggestions with the long --shell option, rather than the glued short option
// (-szsh) that Symfony's templates use.
func TestCompletionScript(t *testing.T) {
	f := newCommandFactory(t, "", "")

	cases := []struct {
		shell string
	}{
		{shell: "zsh"},
		{shell: "bash"},
		{shell: "fish"},
	}

	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			script := f.Run("completion", c.shell)
			assert.Contains(t, script, "--shell="+c.shell)
			assert.NotContains(t, script, "-s"+c.shell)
		})
	}
}

// TestComplete checks that a completion request returns suggestions. The glued
// short options of the completion scripts must reach the legacy CLI unparsed:
// the bundled -h of -szsh used to make the CLI print its help page instead.
func TestComplete(t *testing.T) {
	f := newCommandFactory(t, "", "")

	cases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "short options",
			args:     []string{"_complete", "--no-interaction", "-szsh", "-a1", "-c1", "-iplatform-test", "-ienv"},
			expected: "environment:list",
		},
		{
			name: "long options",
			args: []string{
				"_complete", "--no-interaction", "--shell=zsh", "--api-version=1", "--current=1",
				"--input=platform-test", "--input=env",
			},
			expected: "environment:list",
		},
		{
			// An input token can contain an "h" too, as in "platform-test ssh --pro<TAB>".
			name:     "input containing h",
			args:     []string{"_complete", "--no-interaction", "-szsh", "-a1", "-c2", "-iplatform-test", "-issh", "-i--pro"},
			expected: "--project",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Contains(t, f.Run(c.args...), c.expected)
		})
	}
}

// shellHarnesses print the suggestions of a completion script ($1) for a
// command line ($2), one per line, without an interactive shell.
var shellHarnesses = map[string]string{
	"bash": `source /usr/share/bash-completion/bash_completion
source "$1"
COMP_LINE=$2
COMP_POINT=${#COMP_LINE}
read -ra COMP_WORDS <<< "$COMP_LINE"
[[ $COMP_LINE == *' ' ]] && COMP_WORDS+=('')
COMP_CWORD=$(( ${#COMP_WORDS[@]} - 1 ))
_sf_platform-test
printf '%s\n' "${COMPREPLY[@]}"`,
	// The completion system is stubbed out, to print the values that the script passes to it.
	"zsh": `compdef() {}
_describe() { print -rl -- "${(@)${(@P)2}%%:*}" }
source "$1"
words=(${(z)2})
[[ $2 == *' ' ]] && words+=('')
CURRENT=${#words}
_sf_platform-test`,
	"fish": `source $argv[1]; complete -C $argv[2]`,
}

// TestShellCompletion runs the completion scripts in each shell, for
// suggestions answered in Go (command and option names) and by the legacy CLI
// (argument values).
func TestShellCompletion(t *testing.T) {
	f := newCommandFactory(t, "", "")

	// The scripts run the CLI by the configured executable name.
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(getCommandName(t), filepath.Join(binDir, "platform-test")))
	env := append(testEnv(t.TempDir()), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cases := []struct {
		line     string
		expected string
	}{
		{line: "platform-test ini", expected: "init"},
		{line: "platform-test env:info --pro", expected: "--project"},
		{line: "platform-test completion ", expected: "zsh"},
	}

	for shell, harness := range shellHarnesses {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skipf("%s is not installed", shell)
			}
			if _, err := os.Stat("/usr/share/bash-completion/bash_completion"); shell == "bash" && err != nil {
				t.Skip("bash-completion is not installed")
			}
			script := filepath.Join(t.TempDir(), "completion."+shell)
			require.NoError(t, os.WriteFile(script, []byte(f.Run("completion", shell)), 0o600))

			for _, c := range cases {
				t.Run(c.line, func(t *testing.T) {
					cmd := exec.Command(shell, "-c", harness, shell, script, c.line)
					if shell == "fish" {
						cmd = exec.Command(shell, "-c", harness, script, c.line)
					}
					cmd.Env = env
					out, err := cmd.Output()
					require.NoError(t, err)
					var values []string
					for line := range strings.Lines(string(out)) {
						value, _, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
						values = append(values, value)
					}
					assert.Contains(t, values, c.expected)
				})
			}
		})
	}
}
