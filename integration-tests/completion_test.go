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
// command line ($2) of an executable ($3), one per line, without an
// interactive shell.
var shellHarnesses = map[string]string{
	"bash": `source /usr/share/bash-completion/bash_completion
source "$1"
COMP_LINE=$2
COMP_POINT=${#COMP_LINE}
read -ra COMP_WORDS <<< "$COMP_LINE"
[[ $COMP_LINE == *' ' ]] && COMP_WORDS+=('')
COMP_CWORD=$(( ${#COMP_WORDS[@]} - 1 ))
"_sf_$3"
printf '%s\n' "${COMPREPLY[@]}"`,
	// The completion system is stubbed out, to print the values that the script passes to it.
	"zsh": `compdef() {}
_describe() { print -rl -- "${(@)${(@P)2}%%:*}" }
source "$1"
words=(${(z)2})
[[ $2 == *' ' ]] && words+=('')
CURRENT=${#words}
"_sf_$3"`,
	"fish": `source $argv[1]; complete -C $argv[2]`,
}

// TestShellCompletion runs the completion scripts in each shell, for
// suggestions answered in Go (command and option names) and by the legacy CLI
// (argument values).
//
// It uses the embedded config, as completion in Go relies on the command index
// generated for it.
func TestShellCompletion(t *testing.T) {
	// The binary is named after its configured executable, which the scripts run.
	exe := getCommandName(t)
	name := filepath.Base(exe)
	env := []string{"HOME=" + t.TempDir(), "PATH=" + filepath.Dir(exe) + string(os.PathListSeparator) + os.Getenv("PATH")}

	cases := []struct {
		args     string
		expected string
	}{
		{args: "ini", expected: "init"},
		{args: "env:info --pro", expected: "--project"},
		{args: "completion ", expected: "zsh"},
	}

	for shell, harness := range shellHarnesses {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skipf("%s is not installed", shell)
			}
			if _, err := os.Stat("/usr/share/bash-completion/bash_completion"); shell == "bash" && err != nil {
				t.Skip("bash-completion is not installed")
			}
			gen := exec.Command(exe, "completion", shell)
			gen.Env = env
			b, err := gen.Output()
			require.NoError(t, err)
			script := filepath.Join(t.TempDir(), "completion."+shell)
			require.NoError(t, os.WriteFile(script, b, 0o600))

			for _, c := range cases {
				t.Run(c.args, func(t *testing.T) {
					line := name + " " + c.args
					cmd := exec.Command(shell, "-c", harness, shell, script, line, name)
					if shell == "fish" {
						cmd = exec.Command(shell, "-c", harness, script, line)
					}
					cmd.Env = env
					out, err := cmd.Output()
					require.NoError(t, err)
					var values []string
					for l := range strings.Lines(string(out)) {
						value, _, _ := strings.Cut(strings.TrimSuffix(l, "\n"), "\t")
						values = append(values, value)
					}
					assert.Contains(t, values, c.expected)
				})
			}
		})
	}
}
