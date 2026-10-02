package table

import (
	"bytes"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testTable = &Table{
	Columns: []Column{
		{Header: "ID"},
		{Header: "Title"},
		{Header: "Created", Name: "created_at"},
		{Header: "Status"},
		{Header: "Region 10"},
		{Header: "Region 9"},
	},
	DefaultColumns: []string{"id", "Title", "status"},
}

var testRows = [][]string{
	{"1", "Foo", "2026-01-01", "active", "a", "b"},
	{"2", "Bar, \"Baz\"", "2026-01-02", "inactive"},
	{"3", "Multi\nline\ttab", "2026-01-03", "active", "c", "d"},
}

func TestRender(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	cases := []struct {
		name    string
		opts    Options
		want    string
		wantErr string
	}{
		{
			name: "table defaults",
			opts: Options{},
			want: `+----+-------------+----------+
| ID | Title       | Status   |
+----+-------------+----------+
| 1  | Foo         | active   |
| 2  | Bar, "Baz"  | inactive |
| 3  | Multi       | active   |
|    | line    tab |          |
+----+-------------+----------+
`,
		},
		{
			name: "csv",
			opts: Options{Format: "CSV"},
			want: `ID,Title,Status
1,Foo,active
2,"Bar, ""Baz""",inactive
3,"Multi
line	tab",active
`,
		},
		{
			name: "tsv",
			opts: Options{Format: "tsv"},
			want: "ID\tTitle\tStatus\n1\tFoo\tactive\n2\t\"Bar, \"\"Baz\"\"\"\tinactive\n3\t\"Multi\nline\ttab\"\tactive\n",
		},
		{
			name: "plain without header",
			opts: Options{Format: "plain", NoHeader: true},
			want: "1\tFoo\tactive\n2\tBar, \"Baz\"\tinactive\n3\tMulti line tab\tactive\n",
		},
		{
			name: "columns with wildcard and plus",
			opts: Options{Format: "csv", Columns: []string{"created%+"}},
			want: "Created,ID,Title,Status\n2026-01-01,1,Foo,active\n2026-01-02,2,\"Bar, \"\"Baz\"\"\",inactive\n" +
				"2026-01-03,3,\"Multi\nline\ttab\",active\n",
		},
		{
			name: "repeated columns split by commas and spaces",
			opts: Options{Format: "plain", Columns: []string{"id, region%", "ID status"}},
			want: "ID\tRegion 10\tRegion 9\tStatus\n1\ta\tb\tactive\n2\t\t\tinactive\n3\tc\td\tactive\n",
		},
		{
			name:    "unknown column",
			opts:    Options{Columns: []string{"id,Foo"}},
			wantErr: "Column not found: foo (available columns: created_at, id, region 9, region 10, status, title)",
		},
		{
			name:    "invalid format",
			opts:    Options{Format: "JSON"},
			wantErr: `Invalid format: "json". Supported formats: table, csv, tsv, plain`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			err := testTable.Render(&b, testRows, c.opts)
			if c.wantErr != "" {
				assert.EqualError(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, b.String())
		})
	}
}

func TestRenderTableEdgeCases(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	tbl := &Table{Columns: []Column{{Header: "Name"}, {Header: "Value"}}}
	cases := []struct {
		name string
		rows [][]string
		opts Options
		want string
	}{
		{
			name: "no rows",
			want: "+------+-------+\n| Name | Value |\n+------+-------+\n",
		},
		{
			name: "no header",
			rows: [][]string{{"日本", "x"}},
			opts: Options{NoHeader: true},
			want: "+------+---+\n| 日本 | x |\n+------+---+\n",
		},
		{
			name: "no header or rows",
			opts: Options{NoHeader: true},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			require.NoError(t, tbl.Render(&b, c.rows, c.opts))
			assert.Equal(t, c.want, b.String())
		})
	}
}

func TestRenderProperties(t *testing.T) {
	var b bytes.Buffer
	err := RenderProperties(&b, []string{"id", "email"}, []string{"123", "a@example.com"},
		Options{Format: "csv", Columns: []string{"value"}})
	require.NoError(t, err)
	assert.Equal(t, "Value\n123\na@example.com\n", b.String())
}

func TestFlags(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	testTable.AddFlags(fs)

	columns := fs.Lookup("columns")
	require.NotNil(t, columns)
	assert.Equal(t, "c", columns.Shorthand)
	assert.Equal(t, `Columns to display.
Available columns: id*, title*, status*, created_at, region 9, region 10 (* = default columns).
The character "+" can be used as a placeholder for the default columns.
The % or * characters may be used as a wildcard.
Values may be split by commas (e.g. "a,b,c") and/or whitespace.`, columns.Usage)

	require.NoError(t, fs.Parse([]string{"--format", "csv", "-c", "id", "--columns", "title", "--no-header"}))
	opts, err := OptionsFromFlags(fs)
	require.NoError(t, err)
	assert.Equal(t, Options{Format: "csv", Columns: []string{"id", "title"}, NoHeader: true}, opts)
	assert.True(t, opts.IsMachineReadable())

	// The -c shorthand is not used if it is taken.
	fs = pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.BoolP("cool", "c", false, "")
	PropertiesTable().AddFlags(fs)
	assert.Empty(t, fs.Lookup("columns").Shorthand)
	assert.Contains(t, fs.Lookup("columns").Usage, "Available columns: property, value.")
}

func TestRenderTableWrapping(t *testing.T) {
	cases := []struct {
		name    string
		columns []Column
		rows    [][]string
		width   int
		want    string
	}{
		{
			name:    "wraps to fit",
			columns: []Column{{Header: "ID"}, {Header: "Description"}},
			rows: [][]string{
				{"1", "The quick brown fox jumps over the lazy dog and keeps running far away"},
				{"2", "short"},
			},
			width: 40,
			want: `+----+--------------------------------+
| ID | Description                    |
+----+--------------------------------+
| 1  | The quick brown fox jumps over |
|    | the lazy dog and keeps running |
|    | far away                       |
| 2  | short                          |
+----+--------------------------------+
`,
		},
		{
			name:    "long words and indentation",
			columns: []Column{{Header: "K"}, {Header: "V"}},
			rows: [][]string{
				{"a", "Averyveryveryverylongwordwithoutspaces"},
				{"b", "  indented text that wraps around the cell"},
			},
			width: 30,
			want: `+---+------------------------+
| K | V                      |
+---+------------------------+
| a | Averyveryveryverylongw |
|   | ordwithoutspaces       |
| b |   indented text that   |
|   |   wraps around the     |
|   |   cell                 |
+---+------------------------+
`,
		},
		{
			name:    "no-wrap columns and headers keep their width",
			columns: []Column{{Header: "A long header"}, {Header: "URL", NoWrap: true}, {Header: "Notes"}},
			rows:    [][]string{{"x", "https://example.com/long/path", "some notes that wrap"}},
			width:   40,
			want: `+---------------+-------------------------------+-------+
| A long header | URL                           | Notes |
+---------------+-------------------------------+-------+
| x             | https://example.com/long/path | some  |
|               |                               | notes |
|               |                               | that  |
|               |                               | wrap  |
+---------------+-------------------------------+-------+
`,
		},
		{
			name:    "long words are not broken if the table can fit",
			columns: []Column{{Header: "Row"}, {Header: "Lorem"}, {Header: "ipsum"}, {Header: "dolor"}, {Header: "sit"}},
			rows: [][]string{
				{"#1", "amet", "consectetur", "adipiscing elit", "Quisque pulvinar"},
				{"#2", "tellus sit amet", "sollicitudin", "tincidunt", "risus"},
			},
			width: 60,
			want: `+-----+----------+--------------+------------+----------+
| Row | Lorem    | ipsum        | dolor      | sit      |
+-----+----------+--------------+------------+----------+
| #1  | amet     | consectetur  | adipiscing | Quisque  |
|     |          |              | elit       | pulvinar |
| #2  | tellus   | sollicitudin | tincidunt  | risus    |
|     | sit amet |              |            |          |
+-----+----------+--------------+------------+----------+
`,
		},
		{
			name:    "the unbroken word width is reduced to fit",
			columns: []Column{{Header: "ID"}, {Header: "Title"}, {Header: "Created"}, {Header: "Updated"}, {Header: "Status"}},
			rows: [][]string{{
				"abc123def456", "A project with a reasonably long title for testing",
				"2026-01-01T00:00:00+00:00", "2026-01-02T00:00:00+00:00", "active",
			}},
			width: 80,
			want: `+--------------+------------+--------------------+--------------------+--------+
| ID           | Title      | Created            | Updated            | Status |
+--------------+------------+--------------------+--------------------+--------+
| abc123def456 | A project  | 2026-01-01T00:00:0 | 2026-01-02T00:00:0 | active |
|              | with a     | 0+00:00            | 0+00:00            |        |
|              | reasonably |                    |                    |        |
|              | long title |                    |                    |        |
|              | for        |                    |                    |        |
|              | testing    |                    |                    |        |
+--------------+------------+--------------------+--------------------+--------+
`,
		},
		{
			name:    "indentation is included in the minimum width",
			columns: []Column{{Header: "K"}, {Header: "V"}},
			rows:    [][]string{{"x", "  ab cd"}},
			width:   10,
			want: `+---+------+
| K | V    |
+---+------+
| x |   ab |
|   |   cd |
+---+------+
`,
		},
		{
			name:    "wide characters",
			columns: []Column{{Header: "K"}, {Header: "V"}},
			rows:    [][]string{{"a", "日本語 日本語 日本語 日本語"}},
			width:   20,
			want: `+---+--------+
| K | V      |
+---+--------+
| a | 日本語 |
|   | 日本語 |
|   | 日本語 |
|   | 日本語 |
+---+--------+
`,
		},
		{
			name:    "styles are closed at the end of each line and reopened",
			columns: []Column{{Header: "K"}, {Header: "V"}},
			rows:    [][]string{{"\x1b[1ma\x1b[0m", "\x1b[32mgreen text that wraps\x1b[0m plain"}},
			width:   20,
			want: "+---+------------+\n" +
				"| K | V          |\n" +
				"+---+------------+\n" +
				"| \x1b[1ma\x1b[0m | \x1b[32mgreen text\x1b[0m |\n" +
				"|   | \x1b[32mthat wraps\x1b[0m |\n" +
				"|   | plain      |\n" +
				"+---+------------+\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			tbl := &Table{Columns: c.columns, MaxWidth: c.width}
			require.NoError(t, tbl.Render(&b, c.rows, Options{}))
			assert.Equal(t, c.want, b.String())
		})
	}
}

func TestRenderStripsANSIFromMachineFormats(t *testing.T) {
	tbl := &Table{Columns: []Column{{Header: "\x1b[32mName\x1b[0m"}, {Header: "Value"}}}
	rows := [][]string{{"\x1b[1ma,b\x1b[0m", "\x1b[33mx\x1b[0m"}}
	cases := []struct {
		format string
		want   string
	}{
		{"csv", "Name,Value\n\"a,b\",x\n"},
		{"tsv", "Name\tValue\na,b\tx\n"},
		{"plain", "Name\tValue\na,b\tx\n"},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			var b bytes.Buffer
			require.NoError(t, tbl.Render(&b, rows, Options{Format: c.format}))
			assert.Equal(t, c.want, b.String())
		})
	}
}

func TestTerminalWidth(t *testing.T) {
	t.Setenv("COLUMNS", "123")
	assert.Equal(t, 123, terminalWidth())
}

func TestIsolateStyles(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "unstyled",
			lines: []string{"a", "b"},
			want:  []string{"a", "b"},
		},
		{
			name:  "reset within a line",
			lines: []string{"\x1b[1ma\x1b[0m", "b"},
			want:  []string{"\x1b[1ma\x1b[0m", "b"},
		},
		{
			name:  "style continues to the next line",
			lines: []string{"\x1b[1ma", "b\x1b[m"},
			want:  []string{"\x1b[1ma\x1b[0m", "\x1b[1mb\x1b[m"},
		},
		{
			name:  "extended colors with zero components are not resets",
			lines: []string{"\x1b[1m\x1b[38;2;0;255;0ma", "\x1b[48;5;0mb", "c"},
			want: []string{
				"\x1b[1m\x1b[38;2;0;255;0ma\x1b[0m",
				"\x1b[1m\x1b[38;2;0;255;0m\x1b[48;5;0mb\x1b[0m",
				"\x1b[1m\x1b[38;2;0;255;0m\x1b[48;5;0mc\x1b[0m",
			},
		},
		{
			name:  "reset combined with a new style",
			lines: []string{"\x1b[1ma", "\x1b[0;32mb", "c"},
			want:  []string{"\x1b[1ma\x1b[0m", "\x1b[1m\x1b[0;32mb\x1b[0m", "\x1b[0;32mc\x1b[0m"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isolateStyles(c.lines))
		})
	}
}

func TestRenderColumnLookup(t *testing.T) {
	cases := []struct {
		name string
		tbl  *Table
		opts Options
		want string
	}{
		{
			name: "unknown default column is empty",
			tbl:  &Table{Columns: []Column{{Header: "ID"}, {Header: "Name"}}, DefaultColumns: []string{"name", "status"}},
			opts: Options{Format: "csv"},
			want: "Name,\na,\n",
		},
		{
			name: "duplicate names select the last column",
			tbl:  &Table{Columns: []Column{{Header: "ID"}, {Header: "Name"}, {Header: "Other", Name: "id"}}},
			opts: Options{Format: "csv", Columns: []string{"id"}},
			want: "Other\nx\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			require.NoError(t, c.tbl.Render(&b, [][]string{{"1", "a", "x"}}, c.opts))
			assert.Equal(t, c.want, b.String())
		})
	}
}

func TestIsolateHyperlinks(t *testing.T) {
	const (
		open = "\x1b]8;;https://example.com\x1b\\"
		end  = "\x1b]8;;\x1b\\"
	)
	assert.Equal(t,
		[]string{
			"\x1b[1m" + open + "a" + end + "\x1b[0m",
			"\x1b[1m" + open + "b" + end + "\x1b[0m",
			"\x1b[1m" + open + "c" + end + "\x1b[0m",
		},
		isolateStyles([]string{"\x1b[1m" + open + "a", "b", "c" + end + "\x1b[0m"}),
	)
	assert.Equal(t, []string{open + "a" + end, "b"}, isolateStyles([]string{open + "a" + end, "b"}))
}

func TestWordwrap(t *testing.T) {
	// The expected values match PHP's wordwrap($text, $width, "\n", true), except for wide characters and ANSI sequences.
	cases := []struct {
		text  string
		width int
		want  string
	}{
		{"The quick brown fox", 10, "The quick\nbrown fox"},
		{"double  spaced   words", 8, "double \nspaced  \nwords"},
		{"Averyverylongword", 5, "Avery\nveryl\nongwo\nrd"},
		{"2026-01-01T00:00:00+00:00", 18, "2026-01-01T00:00:0\n0+00:00"},
		{"keep\nbreaks here", 6, "keep\nbreaks\nhere"},
		{"trailing\n", 4, "trai\nling\n\n"},
		{"日本語 日本語", 6, "日本語\n日本語"},
		{"日本語日本語", 5, "日本\n語日\n本語"},
		{"\x1b[32mgreen text\x1b[0m here", 5, "\x1b[32mgreen\ntext\x1b[0m\nhere"},
		{"\x1b[32m12345\x1b[0m", 5, "\x1b[32m12345\x1b[0m"},
		{"\x1b[32m12345\x1b[0m 678", 5, "\x1b[32m12345\x1b[0m\n678"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, wordwrap(c.text, c.width), "wordwrap(%q, %d)", c.text, c.width)
	}
}
