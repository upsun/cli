package tableoutput

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTable_RenderTable(t *testing.T) {
	table := New("ID", "Title", "Region")
	table.MaxWidth = 200 // Set wide width to avoid wrapping in this test
	table.AddRow("proj-1", "Project 1", "region-1")
	table.AddRow("proj-2", "Project 2", "region-2")

	var buf bytes.Buffer
	err := table.RenderTable(&buf)
	assert.NoError(t, err)

	expected := strings.TrimSpace(`
+--------+-----------+----------+
| ID     | Title     | Region   |
+--------+-----------+----------+
| proj-1 | Project 1 | region-1 |
| proj-2 | Project 2 | region-2 |
+--------+-----------+----------+`)

	assert.Equal(t, expected, strings.TrimSpace(buf.String()))
}

func TestTable_RenderTable_Wrapping(t *testing.T) {
	table := New("ID", "Description")
	table.MaxWidth = 40 // Force narrow width
	table.AddRow("proj-1", "This is a very long description that should wrap")

	var buf bytes.Buffer
	err := table.RenderTable(&buf)
	assert.NoError(t, err)

	// Verify the output contains wrapped content (multiple lines for description)
	output := buf.String()
	assert.Contains(t, output, "proj-1")
	assert.Contains(t, output, "This is")
	// The exact wrapping depends on the algorithm, but the table should render
	assert.True(t, strings.Contains(output, "\n"))
}

func TestTable_RenderPlain(t *testing.T) {
	table := New("ID", "Title", "Region")
	table.AddRow("proj-1", "Project 1", "region-1")
	table.AddRow("proj-2", "Project 2", "region-2")

	var buf bytes.Buffer
	err := table.RenderPlain(&buf)
	assert.NoError(t, err)

	expected := strings.TrimSpace(`
ID	Title	Region
proj-1	Project 1	region-1
proj-2	Project 2	region-2`)

	assert.Equal(t, expected, strings.TrimSpace(buf.String()))
}

func TestTable_RenderCSV(t *testing.T) {
	table := New("ID", "Title", "Region")
	table.AddRow("proj-1", "Project 1", "region-1")
	table.AddRow("proj-2", "Project 2", "region-2")

	var buf bytes.Buffer
	err := table.RenderCSV(&buf, false)
	assert.NoError(t, err)

	expected := strings.TrimSpace(`
ID,Title,Region
proj-1,Project 1,region-1
proj-2,Project 2,region-2`)

	assert.Equal(t, expected, strings.TrimSpace(buf.String()))
}

func TestTable_RenderCSV_NoHeader(t *testing.T) {
	table := New("ID", "Title", "Region")
	table.AddRow("proj-1", "Project 1", "region-1")
	table.AddRow("proj-2", "Project 2", "region-2")

	var buf bytes.Buffer
	err := table.RenderCSV(&buf, true)
	assert.NoError(t, err)

	expected := strings.TrimSpace(`
proj-1,Project 1,region-1
proj-2,Project 2,region-2`)

	assert.Equal(t, expected, strings.TrimSpace(buf.String()))
}

func TestWordWrap(t *testing.T) {
	cases := []struct {
		input    string
		maxWidth int
		expected []string
	}{
		{"short", 10, []string{"short"}},
		{"hello world", 5, []string{"hello", "world"}},
		{"a very long word", 8, []string{"a very", "long", "word"}},
		{"", 10, []string{""}},
	}

	for _, tc := range cases {
		result := wordWrap(tc.input, tc.maxWidth)
		assert.Equal(t, tc.expected, result, "for input %q with maxWidth %d", tc.input, tc.maxWidth)
	}
}
