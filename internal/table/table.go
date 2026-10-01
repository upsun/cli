// Package table renders tabular output with the legacy CLI's --format, --columns and --no-header options.
package table

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/pflag"
)

// Output formats.
const (
	FormatTable = "table"
	FormatCSV   = "csv"
	FormatTSV   = "tsv"
	FormatPlain = "plain"
)

// Column describes a table column.
type Column struct {
	// Header is the column's heading.
	Header string
	// Name identifies the column in --columns (case-insensitively). It defaults to Header.
	Name string
	// NoWrap stops the column's cells from being wrapped in the table format.
	NoWrap bool
}

// Table describes the columns of a table.
type Table struct {
	Columns []Column
	// DefaultColumns lists the names of the columns displayed by default. If empty, all columns are displayed.
	DefaultColumns []string
	// MaxWidth is the width that the table format wraps cells to fit. It defaults to the terminal width.
	MaxWidth int
}

// Options holds the user's output options.
type Options struct {
	Format   string
	Columns  []string
	NoHeader bool
}

// AddFlags adds the --format, --columns and --no-header flags.
func (t *Table) AddFlags(fs *pflag.FlagSet) {
	fs.String("format", FormatTable, "The output format: table, csv, tsv, or plain")

	desc := "Columns to display."
	if len(t.Columns) > 0 {
		if len(t.DefaultColumns) > 0 {
			desc += "\nAvailable columns: " + formatAvailableColumns(t.names(), lowerAll(t.DefaultColumns)) +
				" (* = default columns)."
			desc += "\n" + `The character "+" can be used as a placeholder for the default columns.`
		} else {
			desc += "\nAvailable columns: " + formatAvailableColumns(t.names(), nil) + "."
		}
	}
	desc += "\nThe % or * characters may be used as a wildcard." +
		"\n" + `Values may be split by commas (e.g. "a,b,c") and/or whitespace.`
	shorthand := "c"
	if fs.ShorthandLookup(shorthand) != nil {
		shorthand = ""
	}
	fs.StringArrayP("columns", shorthand, nil, desc)

	fs.Bool("no-header", false, "Do not output the table header")
}

// OptionsFromFlags reads the options added by AddFlags.
func OptionsFromFlags(fs *pflag.FlagSet) (Options, error) {
	var (
		opts Options
		err  error
	)
	if opts.Format, err = fs.GetString("format"); err != nil {
		return opts, err
	}
	if opts.Columns, err = fs.GetStringArray("columns"); err != nil {
		return opts, err
	}
	if opts.NoHeader, err = fs.GetBool("no-header"); err != nil {
		return opts, err
	}
	return opts, nil
}

// IsMachineReadable returns whether the format is intended for machines (csv, tsv or plain).
func (o Options) IsMachineReadable() bool {
	switch strings.ToLower(o.Format) {
	case FormatCSV, FormatTSV, FormatPlain:
		return true
	}
	return false
}

// Render writes rows to w. Each row's cells are in the same order as t.Columns; missing cells are empty.
func (t *Table) Render(w io.Writer, rows [][]string, opts Options) error {
	toDisplay, err := t.columnsToDisplay(opts.Columns)
	if err != nil {
		return err
	}

	indexes := make(map[string]int, len(t.Columns))
	for i, name := range t.names() {
		if _, ok := indexes[name]; !ok {
			indexes[name] = i
		}
	}
	filter := func(row []string) []string {
		filtered := make([]string, len(toDisplay))
		for i, name := range toDisplay {
			if j := indexes[name]; j < len(row) {
				filtered[i] = row[j]
			}
		}
		return filtered
	}

	filteredRows := make([][]string, len(rows))
	for i, row := range rows {
		filteredRows[i] = filter(row)
	}
	var header []string
	if !opts.NoHeader {
		headers := make([]string, len(t.Columns))
		for i, c := range t.Columns {
			headers[i] = c.Header
		}
		header = filter(headers)
	}

	switch format := strings.ToLower(opts.Format); format {
	case FormatCSV:
		return renderDelimited(w, header, filteredRows, ",", csvCell(","))
	case FormatTSV:
		return renderDelimited(w, header, filteredRows, "\t", csvCell("\t"))
	case FormatPlain:
		return renderDelimited(w, header, filteredRows, "\t", plainCell)
	case "", FormatTable:
		noWrap := make([]bool, len(toDisplay))
		for i, name := range toDisplay {
			noWrap[i] = t.Columns[indexes[name]].NoWrap
		}
		maxWidth := t.MaxWidth
		if maxWidth <= 0 {
			maxWidth = terminalWidth()
		}
		return renderTable(w, header, filteredRows, noWrap, maxWidth)
	default:
		return fmt.Errorf(`Invalid format: "%s". Supported formats: table, csv, tsv, plain`, format) //nolint:staticcheck
	}
}

// RenderProperties writes a two-column table of property names and values.
func RenderProperties(w io.Writer, names, values []string, opts Options) error {
	rows := make([][]string, len(names))
	for i, name := range names {
		var value string
		if i < len(values) {
			value = values[i]
		}
		rows[i] = []string{name, value}
	}
	return PropertiesTable().Render(w, rows, opts)
}

// PropertiesTable returns the table used by RenderProperties, e.g. to add its flags.
func PropertiesTable() *Table {
	return &Table{Columns: []Column{{Header: "Property"}, {Header: "Value"}}}
}

// names returns the lower-cased name of each column.
func (t *Table) names() []string {
	names := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		name := c.Name
		if name == "" {
			name = c.Header
		}
		names[i] = strings.ToLower(name)
	}
	return names
}

// columnsToDisplay returns the (lower-cased) names of the columns to display.
func (t *Table) columnsToDisplay(specified []string) ([]string, error) {
	available := uniq(t.names())
	defaults := available
	if len(t.DefaultColumns) > 0 {
		defaults = lowerAll(t.DefaultColumns)
	}

	specified = splitColumns(specified)
	if len(specified) == 0 {
		return defaults, nil
	}

	var requested []string
	for _, s := range specified {
		if s == "+" {
			requested = append(requested, defaults...)
		} else {
			requested = append(requested, strings.ToLower(s))
		}
	}

	var toDisplay []string
	for _, r := range requested {
		matched := wildcardSelect(available, r)
		if len(matched) == 0 {
			return nil, fmt.Errorf("Column not found: %s (available columns: %s)",
				r, formatAvailableColumns(available, nil))
		}
		toDisplay = append(toDisplay, matched...)
	}

	return uniq(toDisplay), nil
}

var (
	plusAfterRegex  = regexp.MustCompile(`([\w%])\+`)
	plusBeforeRegex = regexp.MustCompile(`\+([\w%])`)
	splitRegex      = regexp.MustCompile(`[,\s]+`)
)

// splitColumns splits the --columns values by commas and whitespace, and separates a "+" from adjacent names.
func splitColumns(values []string) []string {
	if len(values) == 1 && strings.Contains(values[0], "+") {
		v := plusAfterRegex.ReplaceAllString(values[0], "$1,+")
		v = plusBeforeRegex.ReplaceAllString(v, "+,$1")
		values = []string{v}
	}
	var split []string
	for _, v := range values {
		for _, s := range splitRegex.Split(v, -1) {
			if s != "" {
				split = append(split, s)
			}
		}
	}
	return split
}

// wildcardSelect returns the subjects matching a pattern, in which "%" or "*" match any characters.
func wildcardSelect(subjects []string, wildcard string) []string {
	pattern := strings.NewReplacer("%", ".*", `\*`, ".*").Replace(regexp.QuoteMeta(wildcard))
	re := regexp.MustCompile("^" + pattern + "$")
	var found []string
	for _, s := range subjects {
		if re.MatchString(s) {
			found = append(found, s)
		}
	}
	return found
}

// formatAvailableColumns lists column names, with any defaults first (marked with "*"), then the rest sorted.
func formatAvailableColumns(names, defaults []string) string {
	rest := uniq(names)
	sort.SliceStable(rest, func(i, j int) bool { return naturalLess(rest[i], rest[j]) })
	if len(defaults) == 0 {
		return strings.Join(rest, ", ")
	}
	rest = slices.DeleteFunc(rest, func(n string) bool { return slices.Contains(defaults, n) })
	list := make([]string, 0, len(defaults)+len(rest))
	for _, d := range defaults {
		list = append(list, d+"*")
	}
	return strings.Join(append(list, rest...), ", ")
}

// naturalLess compares strings case-insensitively, treating runs of digits as numbers.
func naturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		if isDigit(a[0]) && isDigit(b[0]) {
			na, ra := leadingDigits(a)
			nb, rb := leadingDigits(b)
			na, nb = strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func leadingDigits(s string) (digits, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

func lowerAll(s []string) []string {
	lowered := make([]string, len(s))
	for i, v := range s {
		lowered[i] = strings.ToLower(v)
	}
	return lowered
}

// uniq returns a copy of s without duplicates, keeping the first occurrence of each.
func uniq(s []string) []string {
	seen := make(map[string]struct{}, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}
