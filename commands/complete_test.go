package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/legacy"
)

func TestParseCompleteRequest(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want *completeRequest
	}{
		{
			name: "short options",
			args: []string{"--no-interaction", "-szsh", "-a1", "-c1", "-iupsun", "-ienv"},
			want: &completeRequest{shell: "zsh", tokens: []string{"upsun", "env"}, current: 1},
		},
		{
			name: "long options",
			args: []string{"--no-interaction", "--shell=bash", "--api-version=1", "--current=2", "--input=upsun",
				"--input=env", "--input=--pro"},
			want: &completeRequest{shell: "bash", tokens: []string{"upsun", "env", "--pro"}, current: 2},
		},
		{
			name: "cursor after the last token",
			args: []string{"-sfish", "-a1", "-iupsun", "-ienv", "-c2"},
			want: &completeRequest{shell: "fish", tokens: []string{"upsun", "env"}, current: 2},
		},
		{name: "separate value", args: []string{"-s", "zsh", "-a1", "-c1", "-iupsun"}},
		{name: "unknown option", args: []string{"-szsh", "-a1", "-c1", "-iupsun", "-S5.4"}},
		{name: "unsupported shell", args: []string{"-spwsh", "-a1", "-c1", "-iupsun"}},
		{name: "old API version", args: []string{"-szsh", "-a0", "-c1", "-iupsun"}},
		{name: "no API version", args: []string{"-szsh", "-c1", "-iupsun"}},
		{name: "cursor on the program", args: []string{"-szsh", "-a1", "-c0", "-iupsun"}},
		{name: "cursor out of range", args: []string{"-szsh", "-a1", "-c3", "-iupsun"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseCompleteRequest(c.args)
			if c.want == nil {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.Equal(t, *c.want, got)
		})
	}
}

func TestCompleteInGo(t *testing.T) {
	cnf := testConfig()
	root := &cobra.Command{Use: "upsun"}
	root.PersistentFlags().Bool("debug", false, "Enable debug logging")
	initCmd := &cobra.Command{Use: "init", Short: "Initialize a project", Aliases: []string{"project:init"}}
	initCmd.Flags().Bool("ai", false, "Use AI")
	initCmd.Flags().String("secret", "", "")
	require.NoError(t, initCmd.Flags().MarkHidden("secret"))
	root.AddCommand(
		initCmd,
		&cobra.Command{Use: "list", Short: "List commands"},
		&cobra.Command{Use: "_complete", Hidden: true},
	)

	legacyCmds := []legacy.Command{
		{
			Name:        "list",
			Description: "Lists commands",
			Definition: legacy.Definition{Options: map[string]legacy.Option{
				"raw": {Name: "--raw", Description: "To output raw command list"},
			}},
		},
		{
			Name:          "project:info",
			Aliases:       []string{"pinfo"},
			HiddenAliases: []string{"project:metadata"},
			Description:   "Read project properties",
			Definition: legacy.Definition{Options: map[string]legacy.Option{
				"project": {Name: "--project", Shortcut: "-p", AcceptValue: true, Description: "The project ID"},
				"refresh": {Name: "--refresh", Description: "Refresh the cache"},
				"legacy":  {Name: "--legacy", Hidden: true},
			}},
		},
		{Name: "project:init", Description: "Replaced by a native command"},
		{Name: "project:list", Aliases: []string{"projects"}, Description: "List projects"},
		{Name: "welcome", Hidden: true},
	}

	globals := []string{"--help", "--no-interaction", "--quiet", "--verbose", "--version", "--yes"}
	projectInfoOpts := []string{
		"--help", "--no-interaction", "--project", "--quiet", "--refresh", "--verbose", "--version", "--yes",
	}
	allCommands := []string{"init", "list", "pinfo", "project:info", "project:init", "project:list", "projects"}

	cases := []struct {
		name    string
		tokens  []string
		current int
		want    []string
		toPHP   bool
	}{
		{name: "no command", tokens: []string{"upsun"}, current: 1, want: allCommands},
		{name: "after a global option", tokens: []string{"upsun", "-v"}, current: 2, want: allCommands},
		{name: "ambiguous command", tokens: []string{"upsun", "pro"}, current: 1, want: allCommands},
		{name: "unknown command", tokens: []string{"upsun", "foo"}, current: 1, want: allCommands},
		{name: "abbreviation", tokens: []string{"upsun", "p:inf"}, current: 1, want: []string{"project:info", "pinfo"}},
		{name: "prefix of a name", tokens: []string{"upsun", "project:inf"}, current: 1, want: []string{"project:info"}},
		{name: "alias", tokens: []string{"upsun", "pinfo"}, current: 1, want: []string{"pinfo"}},
		{name: "hidden alias", tokens: []string{"upsun", "project:metadata"}, current: 1,
			want: []string{"project:info", "pinfo"}},
		{name: "global options", tokens: []string{"upsun", "--"}, current: 1, want: globals},
		{name: "legacy options", tokens: []string{"upsun", "project:info", "--"}, current: 2, want: projectInfoOpts},
		{name: "partial option", tokens: []string{"upsun", "pinfo", "--pro"}, current: 2, want: projectInfoOpts},
		{name: "single dash", tokens: []string{"upsun", "pinfo", "-"}, current: 2, want: projectInfoOpts},
		{name: "option before the command", tokens: []string{"upsun", "-", "pinfo"}, current: 1,
			want: projectInfoOpts},
		{name: "native options", tokens: []string{"upsun", "init", "--"}, current: 2,
			want: []string{"--ai", "--debug", "--help"}},
		{name: "native command replacing a legacy one", tokens: []string{"upsun", "list", "--"}, current: 2,
			want: []string{"--help", "--no-interaction", "--quiet", "--raw", "--verbose", "--version", "--yes"}},
		{name: "native argument", tokens: []string{"upsun", "init"}, current: 2, want: nil},
		{name: "legacy argument", tokens: []string{"upsun", "project:info"}, current: 2, toPHP: true},
		{name: "option value", tokens: []string{"upsun", "pinfo", "--project"}, current: 3, toPHP: true},
		{name: "complete option", tokens: []string{"upsun", "pinfo", "--project"}, current: 2, toPHP: true},
		{name: "option with a value", tokens: []string{"upsun", "pinfo", "--project=a"}, current: 2, toPHP: true},
		{name: "short option", tokens: []string{"upsun", "pinfo", "-pa"}, current: 2, toPHP: true},
		{name: "argument of a command replacing a legacy one", tokens: []string{"upsun", "list"}, current: 2,
			toPHP: true},
		{name: "options of an unknown command", tokens: []string{"upsun", "foo", "--"}, current: 2, toPHP: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := completeInGo(root, cnf, legacyCmds, completeRequest{shell: "zsh", tokens: c.tokens,
				current: c.current})
			if c.toPHP {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			var values []string
			for _, s := range got {
				values = append(values, s.value)
			}
			assert.Equal(t, c.want, values)
		})
	}
}

func TestWriteSuggestions(t *testing.T) {
	suggestions := []suggestion{
		{value: "--columns", description: "Columns to display.\nValues may be split by commas."},
		{value: "--yes"},
	}

	cases := []struct {
		shell       string
		suggestions []suggestion
		want        string
	}{
		{shell: "bash", suggestions: suggestions, want: "--columns\n--yes\n"},
		{shell: "zsh", suggestions: suggestions,
			want: "--columns\tColumns to display. Values may be split by commas.\n--yes\n"},
		{shell: "fish", suggestions: suggestions,
			want: "--columns\tColumns to display. Values may be split by commas.\n--yes"},
		{shell: "bash", want: "\n"},
		{shell: "zsh", want: "\n"},
		{shell: "fish", want: ""},
	}

	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, writeSuggestions(&b, c.shell, c.suggestions))
			assert.Equal(t, c.want, b.String())
		})
	}
}

func TestLegacyConfigOverridden(t *testing.T) {
	cases := []struct {
		name       string
		env        map[string]string
		userConfig bool
		want       bool
	}{
		{name: "default"},
		{name: "unrelated variable", env: map[string]string{"TEST_TOKEN": "abc"}},
		{name: "experiment", env: map[string]string{"TEST_EXPERIMENTAL_ALL_EXPERIMENTS": "1"}, want: true},
		{name: "API setting", env: map[string]string{"TEST_API_SIZING": "0"}, want: true},
		{name: "user config file", userConfig: true, want: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cnf := testConfig()
			home := t.TempDir()
			t.Setenv("TEST_HOME", home)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			if c.userConfig {
				dir := filepath.Join(home, cnf.Application.UserConfigDir)
				require.NoError(t, os.MkdirAll(dir, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("{}"), 0o600))
			}
			assert.Equal(t, c.want, legacyConfigOverridden(cnf))
		})
	}
}
