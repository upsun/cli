// Package tableoutput provides table formatting for CLI output.
package tableoutput

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	defaultTermWidth = 80
	minColumnWidth   = 10
)

// Table represents a table for output.
type Table struct {
	Header   []string
	Rows     [][]string
	MaxWidth int // Maximum table width (0 = auto-detect from terminal)
}

// New creates a new table with the given header.
func New(header ...string) *Table {
	return &Table{
		Header:   header,
		Rows:     make([][]string, 0),
		MaxWidth: 0,
	}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(row ...string) {
	t.Rows = append(t.Rows, row)
}

// getTerminalWidth returns the terminal width or a default value.
func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return defaultTermWidth
	}
	return width
}

// RenderTable renders the table in a box format with borders.
func (t *Table) RenderTable(w io.Writer) error {
	if len(t.Header) == 0 {
		return nil
	}

	maxWidth := t.MaxWidth
	if maxWidth == 0 {
		maxWidth = getTerminalWidth()
	}

	// Calculate natural column widths (without wrapping)
	widths := make([]int, len(t.Header))
	for i, h := range t.Header {
		widths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) {
				// Consider max line width for multi-line cells
				maxLineWidth := maxLineWidth(cell)
				if maxLineWidth > widths[i] {
					widths[i] = maxLineWidth
				}
			}
		}
	}

	// Calculate table overhead (borders: | + space on each side + |)
	// For n columns: n+1 borders (|) + 2*n spaces = 3n+1
	overhead := 3*len(widths) + 1

	// Check if table fits; if not, shrink columns proportionally
	totalWidth := overhead
	for _, w := range widths {
		totalWidth += w
	}

	if totalWidth > maxWidth && maxWidth > overhead {
		widths = t.shrinkColumns(widths, maxWidth-overhead)
	}

	// Wrap cell content to fit column widths
	wrappedHeader := t.wrapRow(t.Header, widths)
	wrappedRows := make([][][]string, len(t.Rows))
	for i, row := range t.Rows {
		wrappedRows[i] = t.wrapRow(row, widths)
	}

	// Build separator line
	sep := make([]string, len(widths))
	for i, w := range widths {
		sep[i] = strings.Repeat("-", w+2)
	}
	separator := "+" + strings.Join(sep, "+") + "+"

	// Print header
	fmt.Fprintln(w, separator)
	t.printWrappedRow(w, wrappedHeader, widths)
	fmt.Fprintln(w, separator)

	// Print rows
	for _, wrappedRow := range wrappedRows {
		t.printWrappedRow(w, wrappedRow, widths)
	}
	fmt.Fprintln(w, separator)

	return nil
}

// shrinkColumns adjusts column widths to fit within availableWidth.
func (t *Table) shrinkColumns(widths []int, availableWidth int) []int {
	total := 0
	for _, w := range widths {
		total += w
	}

	if total <= availableWidth {
		return widths
	}

	// Calculate how much we need to shrink
	excess := total - availableWidth
	newWidths := make([]int, len(widths))
	copy(newWidths, widths)

	// Shrink proportionally, but respect minimum width
	for excess > 0 {
		// Find the widest column that can be shrunk
		maxIdx := -1
		maxWidth := minColumnWidth
		for i, w := range newWidths {
			if w > maxWidth {
				maxWidth = w
				maxIdx = i
			}
		}

		if maxIdx == -1 {
			// All columns at minimum width
			break
		}

		// Shrink the widest column by 1
		newWidths[maxIdx]--
		excess--
	}

	return newWidths
}

// wrapRow wraps each cell in a row to fit its column width.
// Returns a slice of lines for each cell.
func (t *Table) wrapRow(row []string, widths []int) [][]string {
	result := make([][]string, len(widths))
	for i := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		result[i] = wrapText(cell, widths[i])
	}
	return result
}

// printWrappedRow prints a row that may have multi-line cells.
func (t *Table) printWrappedRow(w io.Writer, wrappedCells [][]string, widths []int) {
	// Find the maximum number of lines in any cell
	maxLines := 0
	for _, lines := range wrappedCells {
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}

	if maxLines == 0 {
		maxLines = 1
	}

	// Print each line
	for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
		cells := make([]string, len(widths))
		for colIdx, width := range widths {
			val := ""
			if colIdx < len(wrappedCells) && lineIdx < len(wrappedCells[colIdx]) {
				val = wrappedCells[colIdx][lineIdx]
			}
			cells[colIdx] = fmt.Sprintf(" %-*s ", width, val)
		}
		fmt.Fprintln(w, "|"+strings.Join(cells, "|")+"|")
	}
}

// wrapText wraps text to fit within maxWidth, breaking on word boundaries when possible.
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		maxWidth = minColumnWidth
	}

	// Handle existing newlines
	lines := strings.Split(text, "\n")
	var result []string

	for _, line := range lines {
		if len(line) <= maxWidth {
			result = append(result, line)
			continue
		}

		// Wrap this line
		wrapped := wordWrap(line, maxWidth)
		result = append(result, wrapped...)
	}

	if len(result) == 0 {
		result = []string{""}
	}

	return result
}

// wordWrap wraps a single line at word boundaries.
func wordWrap(text string, maxWidth int) []string {
	if len(text) <= maxWidth {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	currentLine := words[0]
	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) <= maxWidth {
			currentLine += " " + word
		} else {
			// Current line is full
			if len(currentLine) > 0 {
				lines = append(lines, currentLine)
			}
			// If word is longer than maxWidth, break it
			if len(word) > maxWidth {
				for len(word) > maxWidth {
					lines = append(lines, word[:maxWidth])
					word = word[maxWidth:]
				}
				currentLine = word
			} else {
				currentLine = word
			}
		}
	}

	if len(currentLine) > 0 {
		lines = append(lines, currentLine)
	}

	return lines
}

// maxLineWidth returns the maximum line width in a potentially multi-line string.
func maxLineWidth(s string) int {
	maxLen := 0
	for _, line := range strings.Split(s, "\n") {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	return maxLen
}

// RenderPlain renders the table in a plain tab-separated format.
func (t *Table) RenderPlain(w io.Writer) error {
	if len(t.Header) == 0 {
		return nil
	}

	fmt.Fprintln(w, strings.Join(t.Header, "\t"))
	for _, row := range t.Rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	return nil
}

// RenderCSV renders the table as CSV.
func (t *Table) RenderCSV(w io.Writer, noHeader bool) error {
	cw := csv.NewWriter(w)
	if !noHeader {
		if err := cw.Write(t.Header); err != nil {
			return err
		}
	}
	for _, row := range t.Rows {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
