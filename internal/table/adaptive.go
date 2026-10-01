package table

import (
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// minColumnWidth is the width below which columns are not wrapped.
const minColumnWidth = 10

// terminalWidth returns the width of the terminal, from $COLUMNS or the first standard stream that is a terminal.
func terminalWidth() int {
	if w, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && w > 0 {
		return w
	}
	for _, f := range []*os.File{os.Stdout, os.Stderr, os.Stdin} {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

// maxColumnWidths finds the maximum width of each column's content, so that the table fits in maxWidth.
// It is a port of the legacy CLI's AdaptiveTable::getMaxColumnWidths().
// The rows before bodyStart are header rows, which are not wrapped.
func maxColumnWidths(rows [][][]string, bodyStart int, noWrap []bool, maxWidth int) []int {
	count := len(rows[0])
	originalWidths := make([]int, count)
	minWidths := make([]int, count)
	for i, row := range rows {
		for j, lines := range row {
			w := cellWidth(lines)
			originalWidths[j] = max(originalWidths[j], w)
			minCellWidth := minColumnWidth
			if w < minColumnWidth || noWrap[j] || i < bodyStart {
				minCellWidth = w
			}
			minWidths[j] = max(minWidths[j], minCellWidth)
		}
	}

	// Distribute the available width between columns in proportion to their original widths,
	// starting with the narrowest.
	maxContentWidth := float64(maxWidth - (count + 1) - count*2)
	totalWidth := 0
	for _, w := range originalWidths {
		totalWidth += w
	}
	order := make([]int, count)
	for j := range order {
		order[j] = j
	}
	slices.SortStableFunc(order, func(a, b int) int { return originalWidths[a] - originalWidths[b] })

	widths := make([]int, count)
	for _, j := range order {
		var w int
		if totalWidth > 0 {
			w = int(math.Round(maxContentWidth / float64(totalWidth) * float64(originalWidths[j])))
		}
		w = max(w, minWidths[j])
		widths[j] = w
		totalWidth -= originalWidths[j]
		maxContentWidth -= float64(w)
	}
	return widths
}

// cellWidth returns the display width of the longest line in a cell.
func cellWidth(lines []string) int {
	w := 0
	for _, line := range lines {
		w = max(w, ansi.StringWidth(line))
	}
	return w
}

// wrapCell word-wraps a cell's lines to fit the width, keeping any left indentation.
func wrapCell(lines []string, width int) []string {
	contents := strings.Join(lines, "\n")
	trimmed := strings.TrimLeft(contents, " ")
	indent := contents[:len(contents)-len(trimmed)]
	wrapped := strings.Split(ansi.Wrap(trimmed, max(width-len(indent), 1), " "), "\n")
	if indent != "" {
		for i, line := range wrapped {
			wrapped[i] = indent + line
		}
	}
	return wrapped
}

var sgrRegex = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// isolateStyles ends each line with a reset if an ANSI style (SGR) is still active, and re-applies it on the next line.
// This stops styles from leaking into the table's borders and other cells.
func isolateStyles(lines []string) []string {
	active := ""
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = active + line
		for _, m := range sgrRegex.FindAllStringSubmatch(line, -1) {
			if sgrResets(m[1]) {
				active = ""
			}
			if m[1] != "" && m[1] != "0" {
				active += m[0]
			}
		}
		if active != "" {
			out[i] += "\x1b[0m"
		}
	}
	return out
}

// sgrResets returns whether SGR parameters include a reset, skipping the arguments of extended colors.
func sgrResets(params string) bool {
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "", "0":
			return true
		case "38", "48", "58":
			if i+1 < len(parts) && parts[i+1] == "5" {
				i += 2
			} else if i+1 < len(parts) && parts[i+1] == "2" {
				i += 4
			}
		}
	}
	return false
}
