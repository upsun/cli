// Command coverage-report prints a Markdown report of integration test coverage: Go statement coverage per package,
// and which commands and command-specific options the tests run.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const modulePath = "github.com/upsun/cli/"

// excludedPackages are test helpers, which are left out of the Go coverage report.
var excludedPackages = []string{"pkg/mockapi", "pkg/mockssh"}

// globalOptions are left out of the option coverage report.
var globalOptions = []string{
	"help", "quiet", "verbose", "version", "yes", "no", "no-interaction", "ansi", "no-ansi",
	"project", "environment", "app", "host", "identity-file", "wait", "no-wait",
	"format", "columns", "no-header", "date-fmt",
}

// extraCommands are native commands that "list" does not describe.
var extraCommands = []command{
	{Name: "version"},
	{Name: "config:install"},
	{Name: "auth:internal", Hidden: true},
}

func main() {
	commandsFile := flag.String("commands", "", "The output of the CLI's `list --all --format=json`")
	argsLog := flag.String("args", "", "The arguments logged by the integration tests (INTEGRATION_TESTS_ARGS_LOG)")
	coverDir := flag.String("coverdir", "", "The coverage data directory (GOCOVERDIR)")
	flag.Parse()
	if *commandsFile == "" || *argsLog == "" || *coverDir == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(os.Stdout, *commandsFile, *argsLog, *coverDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(w io.Writer, commandsFile, argsLog, coverDir string) error {
	pkgs, err := goCoverage(coverDir)
	if err != nil {
		return err
	}
	cmds, err := loadCommands(commandsFile)
	if err != nil {
		return err
	}
	invocations, err := loadArgs(argsLog)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "## Integration test coverage")
	fmt.Fprintln(w)
	writeGoCoverage(w, pkgs)
	writeCommandCoverage(w, analyze(cmds, invocations))
	return nil
}

type pkgCoverage struct {
	name           string
	covered, total int
}

func (p pkgCoverage) percent() float64 {
	if p.total == 0 {
		return 0
	}
	return 100 * float64(p.covered) / float64(p.total)
}

// goCoverage reads statement coverage per package from a coverage data directory.
func goCoverage(dir string) ([]pkgCoverage, error) {
	tmp, err := os.CreateTemp("", "coverage-*.txt")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	//nolint:gosec // The directory is given by the user.
	cmd := exec.Command("go", "tool", "covdata", "textfmt", "-i="+dir, "-o="+tmp.Name())
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go tool covdata: %w", err)
	}
	f, err := os.Open(tmp.Name())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseProfile(f)
}

// parseProfile sums a text coverage profile's statements per package.
func parseProfile(r io.Reader) ([]pkgCoverage, error) {
	byPkg := map[string]*pkgCoverage{}
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		// Format: file:startLine.startCol,endLine.endCol numStatements count
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid profile line: %q", line)
		}
		file, _, _ := strings.Cut(fields[0], ":")
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("invalid profile line: %q", line)
		}
		name := strings.TrimPrefix(path.Dir(file), modulePath)
		if slices.Contains(excludedPackages, name) {
			continue
		}
		p, ok := byPkg[name]
		if !ok {
			p = &pkgCoverage{name: name}
			byPkg[name] = p
		}
		p.total += stmts
		if count > 0 {
			p.covered += stmts
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	pkgs := make([]pkgCoverage, 0, len(byPkg))
	for _, p := range byPkg {
		pkgs = append(pkgs, *p)
	}
	slices.SortFunc(pkgs, func(a, b pkgCoverage) int { return strings.Compare(a.name, b.name) })
	return pkgs, nil
}

func writeGoCoverage(w io.Writer, pkgs []pkgCoverage) {
	var total pkgCoverage
	for _, p := range pkgs {
		total.covered += p.covered
		total.total += p.total
	}
	fmt.Fprintf(w, "### Go statements: %.1f%%\n\n", total.percent())
	fmt.Fprintln(w, "<details><summary>By package</summary>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Package | Statements | Coverage |")
	fmt.Fprintln(w, "| --- | ---: | ---: |")
	for _, p := range pkgs {
		fmt.Fprintf(w, "| `%s` | %d | %.1f%% |\n", p.name, p.total, p.percent())
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "</details>")
	fmt.Fprintln(w)
}

type command struct {
	Name          string   `json:"name"`
	Aliases       []string `json:"aliases"`
	HiddenAliases []string `json:"hidden_aliases"`
	Hidden        bool     `json:"hidden"`
	Definition    struct {
		Options map[string]option `json:"options"`
	} `json:"definition"`
}

type option struct {
	Name        string `json:"name"`
	Shortcut    string `json:"shortcut"`
	AcceptValue bool   `json:"accept_value"`
	Hidden      bool   `json:"hidden"`
}

func loadCommands(file string) ([]command, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var list struct {
		Commands []command `json:"commands"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	cmds := list.Commands
	for _, extra := range extraCommands {
		if !slices.ContainsFunc(cmds, func(c command) bool { return c.Name == extra.Name }) {
			cmds = append(cmds, extra)
		}
	}
	return cmds, nil
}

func loadArgs(file string) ([][]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var all [][]string
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		var args []string
		if err := json.Unmarshal(s.Bytes(), &args); err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		all = append(all, args)
	}
	return all, s.Err()
}

type commandUsage struct {
	cmd     *command
	runs    int
	options map[string]bool // Used options, by name (without dashes).
}

type analysis struct {
	usage      []*commandUsage // In the order of the commands.
	unresolved map[string]int  // Command names that do not match a command, with their number of runs.
}

// analyze matches invocations to commands and their options.
func analyze(cmds []command, invocations [][]string) analysis {
	a := analysis{unresolved: map[string]int{}}
	for i := range cmds {
		a.usage = append(a.usage, &commandUsage{cmd: &cmds[i], options: map[string]bool{}})
	}
	for _, args := range invocations {
		pos := slices.IndexFunc(args, func(s string) bool { return !strings.HasPrefix(s, "-") })
		if pos == -1 || slices.Contains(args[:pos], "--") {
			continue
		}
		i := resolve(cmds, args[pos])
		if i == -1 {
			a.unresolved[args[pos]]++
			continue
		}
		u := a.usage[i]
		u.runs++
		for _, name := range usedOptions(u.cmd, args[pos+1:]) {
			u.options[name] = true
		}
	}
	return a
}

// resolve finds a command by its name, an alias, or an abbreviation, as Symfony Console does. It returns -1 if none
// or several match.
func resolve(cmds []command, name string) int {
	for i, c := range cmds {
		if c.Name == name || slices.Contains(c.Aliases, name) || slices.Contains(c.HiddenAliases, name) {
			return i
		}
	}
	// Each ":"-separated part of an abbreviation is a prefix, e.g. "e:l" for "environment:list".
	parts := strings.Split(name, ":")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	re := regexp.MustCompile("^" + strings.Join(parts, "[^:]*:") + "[^:]*$")
	var matched, visible []int
	for i, c := range cmds {
		if slices.ContainsFunc(append([]string{c.Name}, c.Aliases...), re.MatchString) {
			matched = append(matched, i)
			if !c.Hidden {
				visible = append(visible, i)
			}
		}
	}
	switch {
	case len(matched) == 1:
		return matched[0]
	case len(visible) == 1:
		return visible[0]
	}
	return -1
}

// usedOptions returns the names of the options in a command's arguments.
func usedOptions(c *command, args []string) []string {
	byShortcut := map[string]option{}
	for name, o := range c.Definition.Options {
		o.Name = name
		for s := range strings.SplitSeq(o.Shortcut, "|") {
			if s = strings.TrimPrefix(s, "-"); s != "" {
				byShortcut[s] = o
			}
		}
	}
	var names []string
	for _, arg := range args {
		switch {
		case arg == "--":
			return names
		case strings.HasPrefix(arg, "--"):
			name, _, _ := strings.Cut(arg[2:], "=")
			names = append(names, name)
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			// Shortcuts may be combined, e.g. "-yq", or followed by a value, e.g. "-emain".
			for _, r := range arg[1:] {
				o, ok := byShortcut[string(r)]
				if !ok {
					break
				}
				names = append(names, o.Name)
				if o.AcceptValue {
					break
				}
			}
		}
	}
	return names
}

func writeCommandCoverage(w io.Writer, a analysis) {
	var (
		visible, run           int
		untested               []string
		options, unusedOptions int
		unusedByCommand        []string
	)
	for _, u := range a.usage {
		if u.cmd.Hidden {
			continue
		}
		visible++
		if u.runs > 0 {
			run++
		} else {
			untested = append(untested, "`"+u.cmd.Name+"`")
		}
		var unused []string
		for name, o := range u.cmd.Definition.Options {
			if o.Hidden || slices.Contains(globalOptions, name) {
				continue
			}
			options++
			if !u.options[name] {
				unused = append(unused, "`--"+name+"`")
			}
		}
		unusedOptions += len(unused)
		if u.runs > 0 && len(unused) > 0 {
			slices.Sort(unused)
			unusedByCommand = append(unusedByCommand,
				fmt.Sprintf("| %#q | %s |", u.cmd.Name, strings.Join(unused, " ")))
		}
	}
	slices.Sort(untested)
	slices.Sort(unusedByCommand)

	fmt.Fprintf(w, "### Commands: %d of %d run\n\n", run, visible)
	if len(untested) > 0 {
		fmt.Fprintf(w, "Not run: %s\n\n", strings.Join(untested, ", "))
	}
	if len(a.unresolved) > 0 {
		names := make([]string, 0, len(a.unresolved))
		for name := range a.unresolved {
			names = append(names, "`"+name+"`")
		}
		slices.Sort(names)
		fmt.Fprintf(w, "Unknown command names: %s\n\n", strings.Join(names, ", "))
	}

	fmt.Fprintf(w, "### Options: %d of %d used\n\n", options-unusedOptions, options)
	fmt.Fprintln(w, "Command-specific options of visible commands, excluding global options.")
	fmt.Fprintln(w)
	if len(unusedByCommand) > 0 {
		fmt.Fprintln(w, "<details><summary>Unused options of commands that are run</summary>")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "| Command | Options |")
		fmt.Fprintln(w, "| --- | --- |")
		for _, line := range unusedByCommand {
			fmt.Fprintln(w, line)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "</details>")
	}
}
