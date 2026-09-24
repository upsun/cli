package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	orderedmap "github.com/wk8/go-ordered-map/v2"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/routeloops"
)

const routeLoopsHelp = "Detects redirect loops in a project's routes. By default this reads " +
	".platform/routes.yaml or .upsun/*.yaml from the current directory. Pass --live " +
	"to fetch the deployed routes via `route:list` instead (requires a deployed environment)."

func innerRouteLoopsCommand(cnf *config.Config) Command {
	noInteractionOption := NoInteractionOption(cnf)

	options := orderedmap.New[string, Option](orderedmap.WithInitialData[string, Option](
		orderedmap.Pair[string, Option]{
			Key: "file",
			Value: Option{
				Name:            "--file",
				Shortcut:        "-f",
				AcceptValue:     true,
				IsValueRequired: true,
				Description:     "Path to a routes.yaml or Upsun config file (with a top-level `routes:` key).",
				Default:         Any{any: ""},
			},
		},
		orderedmap.Pair[string, Option]{
			Key: "live",
			Value: Option{
				Name:        "--live",
				AcceptValue: false,
				Description: "Fetch routes via `route:list` instead of reading the local repo (requires auth + a deployed environment).",
				Default:     Any{any: false},
			},
		},
		orderedmap.Pair[string, Option]{
			Key: "format",
			Value: Option{
				Name:            "--format",
				AcceptValue:     true,
				IsValueRequired: true,
				Description:     "Output format: text or json.",
				Default:         Any{any: "text"},
			},
		},
		orderedmap.Pair[string, Option]{
			Key: "no-fail",
			Value: Option{
				Name:        "--no-fail",
				AcceptValue: false,
				Description: "Exit 0 even when redirect loops are found.",
				Default:     Any{any: false},
			},
		},
		orderedmap.Pair[string, Option]{
			Key: "route-project",
			Value: Option{
				Name:            "--project",
				Shortcut:        "-p",
				AcceptValue:     true,
				IsValueRequired: true,
				Description:     "The project ID (only used with --live).",
				Default:         Any{any: ""},
			},
		},
		orderedmap.Pair[string, Option]{
			Key: "route-environment",
			Value: Option{
				Name:            "--environment",
				Shortcut:        "-e",
				AcceptValue:     true,
				IsValueRequired: true,
				Description:     "The environment ID (only used with --live).",
				Default:         Any{any: ""},
			},
		},
		orderedmap.Pair[string, Option]{
			Key:   HelpOption.GetName(),
			Value: HelpOption,
		},
		orderedmap.Pair[string, Option]{
			Key:   VerboseOption.GetName(),
			Value: VerboseOption,
		},
		orderedmap.Pair[string, Option]{
			Key:   VersionOption.GetName(),
			Value: VersionOption,
		},
		orderedmap.Pair[string, Option]{
			Key:   YesOption.GetName(),
			Value: YesOption,
		},
		orderedmap.Pair[string, Option]{
			Key:   noInteractionOption.GetName(),
			Value: noInteractionOption,
		},
	))

	return Command{
		Name: CommandName{
			Namespace: "route",
			Command:   "loops",
		},
		Usage: []string{
			cnf.Application.Executable + " route:loops",
		},
		Description: "Detect redirect loops in a project's routes",
		Help:        CleanString(routeLoopsHelp),
		Examples: []Example{
			{
				Commandline: "",
				Description: "Check routes.yaml in the current project for redirect loops",
			},
			{
				Commandline: "--live",
				Description: "Check the deployed routes for the current environment",
			},
			{
				Commandline: "--file /path/to/routes.yaml --format=json",
				Description: "Check a specific file, output JSON",
			},
		},
		Definition: Definition{
			Arguments: &orderedmap.OrderedMap[string, Argument]{},
			Options:   options,
		},
		Hidden: false,
	}
}

func newRouteLoopsCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route:loops",
		Short: "Detect redirect loops in a project's routes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRouteLoops(cnf, cmd)
		},
	}

	cmd.Flags().StringP("file", "f", "", "Path to a routes.yaml or Upsun config file")
	cmd.Flags().Bool("live", false, "Fetch routes via `route:list` instead of reading the local repo")
	cmd.Flags().String("format", "text", "Output format: text or json")
	cmd.Flags().Bool("no-fail", false, "Exit 0 even when loops are found")
	cmd.Flags().StringP("route-project", "p", "", "The project ID (only used with --live)")
	cmd.Flags().StringP("route-environment", "e", "", "The environment ID (only used with --live)")

	// Bind each flag individually so we can use distinct viper keys and avoid
	// clashing with any global --project/--environment on other commands.
	for _, name := range []string{"file", "live", "format", "no-fail", "route-project", "route-environment"} {
		_ = viper.BindPFlag("route-loops."+name, cmd.Flags().Lookup(name))
	}

	cmd.SetHelpFunc(func(_ *cobra.Command, _ []string) {
		internalCmd := innerRouteLoopsCommand(cnf)
		fmt.Println(internalCmd.HelpPage(cnf))
	})
	return cmd
}

type routeLoopsJSON struct {
	Cycles   [][]string             `json:"cycles"`
	Warnings []routeLoopsWarningOut `json:"warnings"`
	Source   string                 `json:"source"`
}

type routeLoopsWarningOut struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

func runRouteLoops(cnf *config.Config, cmd *cobra.Command) error {
	format := strings.ToLower(viper.GetString("route-loops.format"))
	if format != "text" && format != "json" {
		return fmt.Errorf("invalid --format %q: expected text or json", format)
	}

	routes, source, err := loadRoutes(cnf, cmd)
	if err != nil {
		return err
	}

	result := routeloops.Detect(routes)

	if format == "json" {
		out := routeLoopsJSON{
			Cycles:   make([][]string, 0, len(result.Cycles)),
			Warnings: make([]routeLoopsWarningOut, 0, len(result.Warnings)),
			Source:   source,
		}
		for _, c := range result.Cycles {
			out.Cycles = append(out.Cycles, c.URLs)
		}
		for _, w := range result.Warnings {
			out.Warnings = append(out.Warnings, routeLoopsWarningOut{URL: w.URL, Reason: w.Reason})
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	} else {
		printRouteLoopsText(cmd, result, source)
	}

	if len(result.Cycles) > 0 && !viper.GetBool("route-loops.no-fail") {
		os.Exit(1)
	}
	return nil
}

func loadRoutes(cnf *config.Config, cmd *cobra.Command) ([]routeloops.Route, string, error) {
	if viper.GetBool("route-loops.live") {
		return loadLiveRoutes(cnf, cmd)
	}
	if path := viper.GetString("route-loops.file"); path != "" {
		routes, err := routeloops.ParseFile(path)
		if err != nil {
			return nil, "", err
		}
		return routes, path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("could not get current working directory: %w", err)
	}
	return routeloops.DiscoverProjectRoutes(cwd)
}

func loadLiveRoutes(cnf *config.Config, cmd *cobra.Command) ([]routeloops.Route, string, error) {
	args := []string{"route:list", "--format=csv", "--columns=route,type,to", "--no-header"}
	if p := viper.GetString("route-loops.route-project"); p != "" {
		args = append(args, "--project="+p)
	}
	if e := viper.GetString("route-loops.route-environment"); e != "" {
		args = append(args, "--environment="+e)
	}

	var buf bytes.Buffer
	wrapper := makeLegacyCLIWrapper(cnf, &buf, cmd.ErrOrStderr(), cmd.InOrStdin())
	if err := wrapper.Exec(cmd.Context(), args...); err != nil {
		return nil, "", fmt.Errorf("fetch routes via legacy CLI: %w", err)
	}
	routes, err := routeloops.ParseLiveCSV(buf.Bytes())
	if err != nil {
		return nil, "", err
	}
	return routes, "live (route:list)", nil
}

func printRouteLoopsText(cmd *cobra.Command, r routeloops.Result, source string) {
	arrow := " -> "
	if useUnicodeArrow() {
		arrow = " → "
	}
	out := cmd.OutOrStdout()
	if source != "" {
		fmt.Fprintf(out, "Source: %s\n", source)
	}
	if len(r.Cycles) == 0 {
		fmt.Fprintln(out, "No redirect loops detected.")
	} else {
		fmt.Fprintf(out, "Detected %d redirect loop(s):\n", len(r.Cycles))
		for _, c := range r.Cycles {
			fmt.Fprintln(out, "  Loop: "+strings.Join(c.URLs, arrow))
		}
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(out, "Warning: %s: %s\n", w.URL, w.Reason)
	}
}

func useUnicodeArrow() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return true
}
