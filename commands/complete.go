package commands

import (
	"cmp"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/legacy"
)

// completeRequest is a request for suggestions from the Symfony completion scripts.
type completeRequest struct {
	shell   string
	tokens  []string // The command line, starting with the program name.
	current int      // The index of the token at the cursor, or len(tokens) after a space.
}

// suggestion is a completion suggestion, with an optional description.
type suggestion struct {
	value       string
	description string
}

// completionCommand is a command that can be completed: native, legacy, or both if a native command replaces a
// legacy one with the same name.
type completionCommand struct {
	names         []string // The command name, followed by its aliases.
	hiddenAliases []string
	description   string
	hidden        bool
	native        *cobra.Command
	legacy        *legacy.Command
}

// completionOption is an option of a command, without its "-" or "--" prefix.
type completionOption struct {
	name        string
	shorthands  []string
	description string
	hidden      bool
}

// parseCompleteRequest parses the arguments that the completion scripts pass to _complete.
// It returns false for any other form, which is left to the legacy CLI.
func parseCompleteRequest(args []string) (r completeRequest, ok bool) {
	var current, apiVersion string
	for _, a := range args {
		if a == "--no-interaction" {
			continue
		}
		v, name, ok := cutCompleteOption(a)
		if !ok {
			return r, false
		}
		switch name {
		case "shell":
			r.shell = v
		case "current":
			current = v
		case "api-version":
			apiVersion = v
		case "input":
			r.tokens = append(r.tokens, v)
		}
	}
	if !slices.Contains([]string{"bash", "zsh", "fish"}, r.shell) || apiVersion != "1" {
		return r, false
	}
	var err error
	if r.current, err = strconv.Atoi(current); err != nil || r.current < 1 || r.current > len(r.tokens) {
		return r, false
	}
	return r, true
}

// cutCompleteOption splits an option of _complete with a glued value, e.g. "-szsh" or "--shell=zsh".
func cutCompleteOption(arg string) (value, name string, ok bool) {
	for _, o := range []struct{ short, long string }{
		{"s", "shell"}, {"c", "current"}, {"a", "api-version"}, {"i", "input"},
	} {
		if v, found := strings.CutPrefix(arg, "--"+o.long+"="); found {
			return v, o.long, v != ""
		}
		if v, found := strings.CutPrefix(arg, "-"+o.short); found && !strings.HasPrefix(arg, "--") {
			return v, o.long, v != ""
		}
	}
	return "", "", false
}

// completeInGo answers a completion request for command or option names, mimicking Symfony's CompleteCommand.
// It returns false for argument and option values, which are left to the legacy CLI.
func completeInGo(
	root *cobra.Command,
	cnf *config.Config,
	legacyCmds []legacy.Command,
	r completeRequest,
) ([]suggestion, bool) {
	cursorFree := r.current == len(r.tokens)
	var atCursor string
	if !cursorFree {
		atCursor = r.tokens[r.current]
	}
	// The legacy CLI's global options take no value, so the first token without a "-" prefix is the command.
	cmdIdx := slices.IndexFunc(r.tokens[1:], func(t string) bool { return t != "" && !strings.HasPrefix(t, "-") })
	if cmdIdx != -1 {
		cmdIdx++
	}
	isOptionName := !cursorFree && strings.HasPrefix(atCursor, "-")

	cmds := completionCommands(root, legacyCmds)
	if cmdIdx == -1 {
		if isOptionName {
			return optionSuggestions(legacyGlobalOptions(cnf)), true
		}
		return commandSuggestions(cmds), true
	}

	c := findCompletionCommand(cmds, r.tokens[cmdIdx])
	if !cursorFree && r.current == cmdIdx {
		if c == nil {
			return commandSuggestions(cmds), true
		}
		// Expand an abbreviation (e.g. "env:i") to the full name or alias.
		names := c.names
		if i := slices.IndexFunc(names, func(n string) bool { return strings.HasPrefix(n, atCursor) }); i != -1 {
			names = names[i : i+1]
		}
		suggestions := make([]suggestion, len(names))
		for i, n := range names {
			suggestions[i] = suggestion{value: n, description: c.description}
		}
		return suggestions, true
	}
	if c == nil {
		return nil, false
	}

	opts := c.options(cnf)
	if isOptionName && !hasOption(opts, atCursor) {
		return optionSuggestions(opts), true
	}
	if c.legacy != nil {
		return nil, false
	}
	// Native commands do not complete values.
	return nil, true
}

// completionCommands lists the native commands and the enabled legacy commands that they do not replace.
func completionCommands(root *cobra.Command, legacyCmds []legacy.Command) []completionCommand {
	legacyByName := make(map[string]*legacy.Command, len(legacyCmds))
	for i := range legacyCmds {
		legacyByName[legacyCmds[i].Name] = &legacyCmds[i]
	}

	var cmds []completionCommand
	nativeNames := map[string]bool{}
	for _, c := range root.Commands() {
		names := append([]string{c.Name()}, c.Aliases...)
		for _, n := range names {
			nativeNames[n] = true
		}
		cmds = append(cmds, completionCommand{
			names:       names,
			description: c.Short,
			hidden:      c.Hidden,
			native:      c,
			legacy:      legacyByName[c.Name()],
		})
	}
	for i := range legacyCmds {
		c := &legacyCmds[i]
		if nativeNames[c.Name] {
			continue
		}
		cmds = append(cmds, completionCommand{
			names:         append([]string{c.Name}, c.Aliases...),
			hiddenAliases: c.HiddenAliases,
			description:   c.Description,
			hidden:        c.Hidden,
			legacy:        c,
		})
	}
	return cmds
}

// findCompletionCommand finds a command by its name, an alias or an abbreviation.
func findCompletionCommand(cmds []completionCommand, name string) *completionCommand {
	candidates := make([]abbrevCandidate, len(cmds))
	for i, c := range cmds {
		if slices.Contains(c.names, name) || slices.Contains(c.hiddenAliases, name) {
			return &cmds[i]
		}
		candidates[i] = abbrevCandidate{names: c.names, hidden: c.hidden}
	}
	if i := resolveAbbreviation(name, candidates); i != -1 {
		return &cmds[i]
	}
	return nil
}

// commandSuggestions suggests the names and aliases of the visible commands.
func commandSuggestions(cmds []completionCommand) []suggestion {
	var suggestions []suggestion
	for _, c := range cmds {
		if c.hidden {
			continue
		}
		for _, n := range c.names {
			suggestions = append(suggestions, suggestion{value: n, description: c.description})
		}
	}
	slices.SortFunc(suggestions, func(a, b suggestion) int { return cmp.Compare(a.value, b.value) })
	return suggestions
}

// options lists the command's options, including global ones. A legacy definition takes precedence, as a native
// command with the same name hands its input to the legacy CLI.
func (c *completionCommand) options(cnf *config.Config) []completionOption {
	if c.legacy == nil {
		c.native.InitDefaultHelpFlag()
		return append(flagOptions(c.native.LocalFlags()), flagOptions(c.native.InheritedFlags())...)
	}
	opts := legacyGlobalOptions(cnf)
	for _, o := range c.legacy.Definition.Options {
		opts = append(opts, newCompletionOption(o.Name, o.Shortcut, o.Description, o.Hidden))
	}
	return opts
}

// legacyGlobalOptions lists the legacy CLI's global options, which its commands' definitions leave out.
func legacyGlobalOptions(cnf *config.Config) []completionOption {
	globals := globalOptions(cnf)
	opts := make([]completionOption, 0, len(globals))
	for _, o := range globals {
		// The list hides --quiet, unlike the legacy CLI's input definition.
		hidden := o.Hidden && o.Name != QuietOption.Name
		opts = append(opts, newCompletionOption(o.Name, o.Shortcut, string(o.Description), hidden))
	}
	return opts
}

// newCompletionOption converts a Symfony option, e.g. with the name "--verbose" and the shortcut "-v|vv|vvv".
func newCompletionOption(name, shortcut, description string, hidden bool) completionOption {
	var shorthands []string
	for s := range strings.SplitSeq(shortcut, "|") {
		if s = strings.TrimLeft(s, "-"); s != "" {
			shorthands = append(shorthands, s[:1])
		}
	}
	return completionOption{
		name:        strings.TrimPrefix(name, "--"),
		shorthands:  shorthands,
		description: description,
		hidden:      hidden,
	}
}

func flagOptions(fs *pflag.FlagSet) []completionOption {
	var opts []completionOption
	fs.VisitAll(func(f *pflag.Flag) {
		o := completionOption{name: f.Name, description: f.Usage, hidden: f.Hidden}
		if f.Shorthand != "" {
			o.shorthands = []string{f.Shorthand}
		}
		opts = append(opts, o)
	})
	return opts
}

// hasOption tests if a token such as "--project=foo" or "-pfoo" names one of the options, like Symfony's
// CompletionInput. If not, option names are suggested.
func hasOption(opts []completionOption, token string) bool {
	token, _, _ = strings.Cut(token, "=")
	if name, ok := strings.CutPrefix(token, "--"); ok {
		return name != "" && slices.ContainsFunc(opts, func(o completionOption) bool { return o.name == name })
	}
	if len(token) < 2 {
		return false
	}
	return slices.ContainsFunc(opts, func(o completionOption) bool { return slices.Contains(o.shorthands, token[1:2]) })
}

// optionSuggestions suggests the visible options.
func optionSuggestions(opts []completionOption) []suggestion {
	var suggestions []suggestion
	for _, o := range opts {
		if !o.hidden {
			suggestions = append(suggestions, suggestion{value: "--" + o.name, description: o.description})
		}
	}
	slices.SortFunc(suggestions, func(a, b suggestion) int { return cmp.Compare(a.value, b.value) })
	return slices.CompactFunc(suggestions, func(a, b suggestion) bool { return a.value == b.value })
}

// writeSuggestions writes suggestions in the format of Symfony's completion output for the shell.
func writeSuggestions(w io.Writer, shell string, suggestions []suggestion) error {
	lines := make([]string, len(suggestions))
	for i, s := range suggestions {
		lines[i] = s.value
		if shell != "bash" && s.description != "" {
			// Each suggestion must fit on one line.
			lines[i] += "\t" + strings.Join(strings.Fields(s.description), " ")
		}
	}
	out := strings.Join(lines, "\n")
	if shell != "fish" {
		out += "\n"
	}
	_, err := io.WriteString(w, out)
	return err
}
