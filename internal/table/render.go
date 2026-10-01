package table

import (
	"io"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Line breaks matched by PCRE's \R.
var lineBreakRegex = regexp.MustCompile("\r\n|[\n\v\f\r\u0085  ]")

// csvCell returns a function formatting a cell like the legacy CLI's Csv class, with LF line breaks.
// ANSI escape sequences are removed.
func csvCell(delimiter string) func(string) string {
	return func(cell string) string {
		cell = ansi.Strip(cell)
		if strings.ContainsAny(cell, `"`+"\n"+delimiter) {
			cell = `"` + strings.ReplaceAll(cell, `"`, `""`) + `"`
		}
		return lineBreakRegex.ReplaceAllString(cell, "\n")
	}
}

var plainReplaceRegex = regexp.MustCompile(`[\r\n\t]+`)

// plainCell formats a cell for the plain format, removing ANSI escape sequences and replacing newlines and tabs.
func plainCell(cell string) string {
	return plainReplaceRegex.ReplaceAllString(ansi.Strip(cell), " ")
}

func renderDelimited(
	w io.Writer, header []string, rows [][]string, delimiter string, formatCell func(string) string,
) error {
	if header != nil {
		rows = append([][]string{header}, rows...)
	}
	var b strings.Builder
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				b.WriteString(delimiter)
			}
			b.WriteString(formatCell(cell))
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// renderTable writes a table with borders, in the default style of Symfony Console tables.
// Cells are wrapped so that the table fits in maxWidth, unless their column is in noWrap.
func renderTable(w io.Writer, header []string, rows [][]string, noWrap []bool, maxWidth int) error {
	all := rows
	if header != nil {
		all = append([][]string{header}, rows...)
	}
	if len(all) == 0 || len(all[0]) == 0 {
		return nil
	}

	// Split each cell into lines.
	split := make([][][]string, len(all))
	for i, row := range all {
		split[i] = make([][]string, len(row))
		for j, cell := range row {
			// Tabs are replaced, as their display width depends on their position.
			cell = strings.ReplaceAll(strings.ReplaceAll(cell, "\r\n", "\n"), "\t", "    ")
			split[i][j] = strings.Split(cell, "\n")
		}
	}

	// Wrap the rows (not the header) to fit the maximum width.
	bodyStart := 0
	if header != nil {
		bodyStart = 1
	}
	maxColumnWidths := maxColumnWidths(split, bodyStart, noWrap, maxWidth)
	for _, row := range split[bodyStart:] {
		for j, lines := range row {
			if cellWidth(lines) > maxColumnWidths[j] {
				row[j] = wrapCell(lines, maxColumnWidths[j])
			}
		}
	}

	// Find the width of each column, and stop styles from continuing to other cells.
	widths := make([]int, len(all[0]))
	for _, row := range split {
		for j, lines := range row {
			row[j] = isolateStyles(lines)
			widths[j] = max(widths[j], cellWidth(lines))
		}
	}

	var b strings.Builder
	separator := func() {
		b.WriteString("+")
		for _, width := range widths {
			b.WriteString(strings.Repeat("-", width+2) + "+")
		}
		b.WriteString("\n")
	}
	writeRow := func(cells [][]string) {
		height := 0
		for _, lines := range cells {
			height = max(height, len(lines))
		}
		for l := range height {
			b.WriteString("|")
			for j, lines := range cells {
				var line string
				if l < len(lines) {
					line = lines[l]
				}
				b.WriteString(" " + line + strings.Repeat(" ", widths[j]-ansi.StringWidth(line)) + " |")
			}
			b.WriteString("\n")
		}
	}

	separator()
	if header != nil {
		writeRow(split[0])
		separator()
		split = split[1:]
	}
	for _, cells := range split {
		writeRow(cells)
	}
	if len(split) > 0 {
		separator()
	}

	_, err := io.WriteString(w, b.String())
	return err
}
