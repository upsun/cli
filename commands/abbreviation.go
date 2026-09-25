package commands

import (
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/legacy"
)

// abbrevCandidate is a command that an abbreviation may resolve to.
type abbrevCandidate struct {
	names  []string // The command name, followed by its aliases.
	hidden bool
	native bool
}

// expandAbbreviation replaces an abbreviated native command name in args (e.g. "p:init") with the full name.
//
// Abbreviations are resolved the same way as the legacy CLI (Symfony Console's Application::find()), across both
// native and legacy commands. Only a unique match to a native command is expanded, as anything else is handled by the
// legacy CLI. The legacy commands are only loaded if needed.
func expandAbbreviation(
	root *cobra.Command,
	loadLegacyCmds func() ([]legacy.Command, error),
	args []string,
) (expanded []string, ok bool, err error) {
	// Only boolean root flags may precede the command, so that a flag's value is not mistaken for it.
	pos := slices.IndexFunc(args, func(a string) bool { return !isRootBoolFlag(root, a) })
	if pos == -1 || strings.HasPrefix(args[pos], "-") {
		return nil, false, nil
	}
	name := args[pos]

	var candidates []abbrevCandidate
	nativeNames := map[string]bool{}
	for _, c := range root.Commands() {
		names := append([]string{c.Name()}, c.Aliases...)
		for _, n := range names {
			nativeNames[n] = true
		}
		candidates = append(candidates, abbrevCandidate{names: names, hidden: c.Hidden, native: true})
	}
	if nativeNames[name] || resolveAbbreviation(name, candidates) == nil {
		return nil, false, nil
	}

	legacyCmds, err := loadLegacyCmds()
	if err != nil {
		return nil, false, err
	}
	for _, c := range legacyCmds {
		if nativeNames[c.Name] {
			// Overridden by a native command.
			continue
		}
		names := append([]string{c.Name}, c.Aliases...)
		if slices.Contains(names, name) || slices.Contains(c.HiddenAliases, name) {
			return nil, false, nil
		}
		candidates = append(candidates, abbrevCandidate{names: names, hidden: c.Hidden})
	}

	target := resolveAbbreviation(name, candidates)
	if target == nil || !target.native {
		return nil, false, nil
	}
	expanded = slices.Clone(args)
	expanded[pos] = target.names[0]
	return expanded, true, nil
}

// isRootBoolFlag tests if arg consists of boolean root flags, e.g. "--yes" or "-vq".
func isRootBoolFlag(root *cobra.Command, arg string) bool {
	flagSets := []*pflag.FlagSet{root.Flags(), root.PersistentFlags()}
	isBool := func(lookup func(*pflag.FlagSet) *pflag.Flag) bool {
		for _, fs := range flagSets {
			if f := lookup(fs); f != nil {
				return f.Value.Type() == "bool"
			}
		}
		return false
	}
	if name, ok := strings.CutPrefix(arg, "--"); ok {
		name, _, _ = strings.Cut(name, "=")
		return isBool(func(fs *pflag.FlagSet) *pflag.Flag { return fs.Lookup(name) })
	}
	shorthands, ok := strings.CutPrefix(arg, "-")
	if !ok || shorthands == "" {
		return false
	}
	for _, c := range shorthands {
		if !isBool(func(fs *pflag.FlagSet) *pflag.Flag { return fs.ShorthandLookup(string(c)) }) {
			return false
		}
	}
	return true
}

// enabledLegacyCommands returns a loader for the legacy commands that are not disabled by config.
func enabledLegacyCommands(cnf *config.Config, load func() ([]legacy.Command, error)) func() ([]legacy.Command, error) {
	return func() ([]legacy.Command, error) {
		cmds, err := load()
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(slices.Clone(cmds), func(c legacy.Command) bool {
			return slices.Contains(cnf.Application.DisabledCommands, c.Name) ||
				slices.Contains(cnf.Application.WrappedDisabledCommands, c.Name)
		}), nil
	}
}

// resolveAbbreviation follows Symfony Console's rules to find the command abbreviated by name, if it is unique.
func resolveAbbreviation(name string, candidates []abbrevCandidate) *abbrevCandidate {
	parts := strings.Split(name, ":")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	expr := "^" + strings.Join(parts, "[^:]*:") + "[^:]*"

	matchAll := func(re *regexp.Regexp) (matched []int, fullMatch bool) {
		for i, c := range candidates {
			isMatch := false
			for _, n := range c.names {
				if loc := re.FindStringIndex(n); loc != nil {
					isMatch = true
					fullMatch = fullMatch || loc[1] == len(n)
				}
			}
			if isMatch {
				matched = append(matched, i)
			}
		}
		return matched, fullMatch
	}

	// Try a case-sensitive match first, then case-insensitive.
	matched, fullMatch := matchAll(regexp.MustCompile(expr))
	if len(matched) == 0 {
		matched, fullMatch = matchAll(regexp.MustCompile("(?i)" + expr))
	}
	// Prefix-only matches (e.g. "project" for "project:variable:get") count toward ambiguity, but at least one
	// command must match fully.
	if !fullMatch {
		return nil
	}

	// Hidden commands still count toward ambiguity: the legacy CLI's lazy-loaded commands do not reliably report
	// whether they are hidden, so it can resolve to them.
	if len(matched) != 1 || candidates[matched[0]].hidden {
		return nil
	}
	return &candidates[matched[0]]
}
