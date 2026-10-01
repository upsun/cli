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
	cases := []struct {
		name    string
		opts    Options
		want    string
		wantErr string
	}{
		{
			name: "table defaults",
			opts: Options{},
			want: `+----+------------+----------+
| ID | Title      | Status   |
+----+------------+----------+
| 1  | Foo        | active   |
| 2  | Bar, "Baz" | inactive |
| 3  | Multi      | active   |
|    | line	tab    |          |
+----+------------+----------+
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
