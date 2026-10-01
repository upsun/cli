package commands

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	orderedmap "github.com/wk8/go-ordered-map/v2"

	"github.com/upsun/cli/internal/config"
)

// commandFromCobra describes a native Cobra command in the same format as the legacy CLI's commands.
func commandFromCobra(cnf *config.Config, c *cobra.Command) Command {
	namespace, name, ok := strings.Cut(c.Name(), ":")
	if !ok {
		namespace, name = "", c.Name()
	}
	options := orderedmap.New[string, Option]()
	c.Flags().VisitAll(func(f *pflag.Flag) {
		opt := Option{
			Name:        "--" + f.Name,
			Description: CleanString(f.Usage),
			Hidden:      f.Hidden,
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
		}
		options.Set(f.Name, opt)
	})
	for _, opt := range globalOptions(cnf) {
		options.Set(opt.GetName(), opt)
	}
	return Command{
		Name:        CommandName{Namespace: namespace, Command: name},
		Usage:       []string{fmt.Sprintf("%s %s", cnf.Application.Executable, c.Name())},
		Aliases:     c.Aliases,
		Description: CleanString(c.Short),
		Help:        CleanString(c.Long),
		Definition: Definition{
			Arguments: orderedmap.New[string, Argument](),
			Options:   options,
		},
		Hidden: c.Hidden,
	}
}

// useLegacyStyleHelp makes a native command print its help in the same format as the legacy CLI's commands.
func useLegacyStyleHelp(cnf *config.Config, c *cobra.Command) *cobra.Command {
	c.SetHelpFunc(func(c *cobra.Command, _ []string) {
		desc := commandFromCobra(cnf, c)
		fmt.Fprintln(c.OutOrStdout(), desc.HelpPage(cnf))
		if c.Example != "" {
			fmt.Fprintln(c.OutOrStdout(), color.YellowString("Examples:"))
			fmt.Fprintln(c.OutOrStdout(), c.Example)
		}
	})
	return c
}
