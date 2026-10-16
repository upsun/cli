package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/upsun/cli/internal/config"
)

const (
	// legacyStyleAnnotation marks a native command that is described like the legacy CLI's commands.
	legacyStyleAnnotation = "legacy-style"
	// argAnnotationPrefix prefixes a Cobra annotation holding an argument's description, e.g. "arg:path".
	argAnnotationPrefix = "arg:"
)

// commandFromCobra describes a native Cobra command in the same format as the legacy CLI's commands.
func commandFromCobra(cnf *config.Config, c *cobra.Command) Command {
	namespace, name, ok := strings.Cut(c.Name(), ":")
	if !ok {
		namespace, name = "", c.Name()
	}
	var options orderedList[Option]
	c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
		// Cobra's help flag is replaced by the global --help option.
		if f.Hidden || f.Name == HelpOption.GetName() {
			return
		}
		opt := Option{
			Name:        "--" + f.Name,
			Description: CleanString(f.Usage),
		}
		if f.Shorthand != "" {
			opt.Shortcut = "-" + f.Shorthand
		}
		switch f.Value.Type() {
		case "bool":
			opt.Default = Any{false}
		case "stringSlice":
			opt.AcceptValue, opt.IsValueRequired, opt.IsMultiple = true, true, true
			opt.Default = Any{[]any{}}
		default:
			opt.AcceptValue, opt.IsValueRequired = true, true
			opt.Default = Any{nil}
			if f.DefValue != "" {
				opt.Default = Any{f.DefValue}
			}
		}
		options = append(options, opt)
	})
	for _, opt := range globalOptions(cnf) {
		if !opt.Hidden {
			options = append(options, opt)
		}
	}
	cmd := Command{
		Name:        CommandName{Namespace: namespace, Command: name},
		Aliases:     c.Aliases,
		Description: CleanString(c.Short),
		Help:        CleanString(c.Long),
		Definition: Definition{
			Arguments: argumentsFromCobra(c),
			Options:   options,
		},
		Hidden: c.Hidden,
	}
	usage := cnf.Application.Executable + " " + cmd.PreferredName()
	for _, arg := range cmd.Definition.Arguments {
		synopsis := "<" + arg.Name + ">"
		if arg.IsArray {
			synopsis += "..."
		}
		if !arg.IsRequired {
			synopsis = "[" + synopsis + "]"
		}
		usage += " " + synopsis
	}
	cmd.Usage = []string{usage}
	cmd.Examples = examplesFromCobra(cnf, c)
	return cmd
}

// argumentsFromCobra parses the arguments in a command's Use line, e.g. "<id>", "[path]" or "[files...]".
func argumentsFromCobra(c *cobra.Command) orderedList[Argument] {
	var args orderedList[Argument]
	for _, field := range strings.Fields(c.Use)[1:] {
		if field == "[flags]" {
			continue
		}
		name, optional := strings.CutPrefix(field, "[")
		name = strings.TrimSuffix(name, "]")
		name, isArray := strings.CutSuffix(name, "...")
		name = strings.Trim(name, "<>")
		args = append(args, Argument{
			Name:        name,
			IsRequired:  YesNo(!optional),
			IsArray:     YesNo(isArray),
			Description: CleanString(c.Annotations[argAnnotationPrefix+name]),
		})
	}
	return args
}

// examplesFromCobra parses a command's Example text, written as "# Description" lines each followed by a command line.
func examplesFromCobra(cnf *config.Config, c *cobra.Command) []Example {
	var (
		examples    []Example
		description string
	)
	for line := range strings.Lines(c.Example) {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			description = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		default:
			examples = append(examples, Example{
				Commandline: trimCommandName(cnf, c, line),
				Description: CleanString(description),
			})
		}
	}
	return examples
}

// trimCommandName removes the executable and command name (or alias) from the start of a command line.
func trimCommandName(cnf *config.Config, c *cobra.Command, line string) string {
	rest, ok := strings.CutPrefix(line, cnf.Application.Executable+" ")
	if !ok {
		return line
	}
	for _, name := range append([]string{c.Name()}, c.Aliases...) {
		if rest == name {
			return ""
		}
		if args, ok := strings.CutPrefix(rest, name+" "); ok {
			return args
		}
	}
	return line
}

// useLegacyStyleHelp makes a native command print its help, and appear in "list", like the legacy CLI's commands.
func useLegacyStyleHelp(cnf *config.Config, c *cobra.Command) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[legacyStyleAnnotation] = "1"
	c.SetHelpFunc(func(c *cobra.Command, _ []string) {
		desc := commandFromCobra(cnf, c)
		fmt.Fprintln(c.OutOrStdout(), desc.HelpPage(cnf))
	})
	return c
}
