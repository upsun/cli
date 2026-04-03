// commands/cobrahelp/help.go
package cobrahelp

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SetPhpStyle sets a PHP CLI-style SetHelpFunc on cmd.
// Call this on each new Go command after creating it.
func SetPhpStyle(cmd *cobra.Command) {
	cmd.SetHelpFunc(func(c *cobra.Command, _ []string) {
		c.Print(RenderHelp(c))
	})
}

// RenderHelp renders a PHP CLI-style help page for the given Cobra command.
func RenderHelp(cmd *cobra.Command) string {
	var b bytes.Buffer
	w := tabwriter.NewWriter(&b, 0, 8, 1, ' ', 0)

	// Command name: first word of Use, then rest of Use is the signature.
	name := cmd.Use
	if idx := strings.Index(name, " "); idx != -1 {
		name = name[:idx]
	}
	fmt.Fprintln(w, color.YellowString("Command: ")+name)
	fmt.Fprintln(w, color.YellowString("Description: ")+cmd.Short)
	fmt.Fprintln(w, "")

	// Usage line: executable + Use.
	root := cmd
	for root.HasParent() {
		root = root.Parent()
	}
	binary := root.Use
	fmt.Fprintln(w, color.YellowString("Usage:"))
	fmt.Fprintln(w, " "+binary+" "+cmd.Use)
	if len(cmd.Aliases) > 0 {
		for _, alias := range cmd.Aliases {
			fmt.Fprintln(w, " "+binary+" "+alias)
		}
	}
	fmt.Fprintln(w, "")

	// Options: local flags only (not inherited).
	hasFlags := false
	cmd.Flags().VisitAll(func(_ *pflag.Flag) {
		hasFlags = true
	})
	if hasFlags {
		fmt.Fprintln(w, color.YellowString("Options:"))
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			shorthand := ""
			if f.Shorthand != "" {
				shorthand = color.GreenString("-"+f.Shorthand) + ","
			} else {
				shorthand = "   "
			}
			longName := color.GreenString("--" + f.Name)
			fmt.Fprintf(w, "  %s %s\t%s\n", shorthand, longName, f.Usage)
		})
		fmt.Fprintln(w, "")
	}

	w.Flush()
	return b.String()
}
