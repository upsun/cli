package table

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// Words up to maxUnbrokenWidth are not broken when wrapping if the table can fit, and words up to
// minUnbrokenWidth are not broken even if it does not.
const (
	maxUnbrokenWidth = 20
	minUnbrokenWidth = 10
)

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
//
// This is similar to a web browser's automatic table layout, and matches the legacy CLI's
// AdaptiveTable::getMaxColumnWidths(). Each column has a maximum width (its widest cell) and a minimum width
// (its longest word, up to maxUnbrokenWidth, or less if needed to fit). If the maximum widths do not fit, each
// column gets its minimum width plus a share of the remaining space, in proportion to the difference between its
// maximum and minimum.
// The rows before bodyStart are header rows, which are not wrapped.
func maxColumnWidths(rows [][][]string, bodyStart int, noWrap []bool, maxWidth int) []int {
	count := len(rows[0])
	maxWidths := make([]int, count)
	fixedWidths := make([]int, count)
	wordWidths := make([]int, count)
	for i, row := range rows {
		for j, lines := range row {
			w := cellWidth(lines)
			maxWidths[j] = max(maxWidths[j], w)
			if noWrap[j] || i < bodyStart {
				fixedWidths[j] = max(fixedWidths[j], w)
			} else {
				wordWidths[j] = max(wordWidths[j], longestWordWidth(lines))
			}
		}
	}

	available := maxWidth - (count + 1) - count*2
	maxTotal := sum(maxWidths)
	if maxTotal <= available {
		return maxWidths
	}

	// The minimum column width is the width of the longest word, capped at maxUnbrokenWidth.
	// The cap is reduced, down to minUnbrokenWidth, until the minimum widths fit.
	minWidths := make([]int, count)
	minTotal := 0
	for limit := maxUnbrokenWidth; ; limit-- {
		for j := range minWidths {
			minWidths[j] = max(fixedWidths[j], min(wordWidths[j], limit))
		}
		minTotal = sum(minWidths)
		if minTotal <= available || limit <= minUnbrokenWidth {
			break
		}
	}
	if minTotal >= available {
		return minWidths
	}

	// Share the extra space in proportion to each column's flexibility (its maximum minus its minimum width).
	// The remainder is given to the columns with the largest fractional parts, in column order for ties.
	extra, flexTotal := available-minTotal, maxTotal-minTotal
	widths := make([]int, count)
	remainders := make([]int, count)
	for j := range widths {
		share := (maxWidths[j] - minWidths[j]) * extra
		widths[j] = minWidths[j] + share/flexTotal
		remainders[j] = share % flexTotal
	}
	order := make([]int, count)
	for j := range order {
		order[j] = j
	}
	slices.SortStableFunc(order, func(a, b int) int { return remainders[b] - remainders[a] })
	for _, j := range order[:available-sum(widths)] {
		widths[j]++
	}
	return widths
}

func sum(s []int) int {
	total := 0
	for _, v := range s {
		total += v
	}
	return total
}

// longestWordWidth returns the display width of the longest whitespace-separated word in a cell.
func longestWordWidth(lines []string) int {
	w := 0
	for _, line := range lines {
		for _, word := range strings.Fields(ansi.Strip(line)) {
			w = max(w, ansi.StringWidth(word))
		}
	}
	return w
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
	wrapped := strings.Split(wordwrap(trimmed, max(width-len(indent), 1)), "\n")
	if indent != "" {
		for i, line := range wrapped {
			wrapped[i] = indent + line
		}
	}
	return wrapped
}

// wordwrap wraps text to the width, like PHP's wordwrap($text, $width, "\n", true).
// It is a port of PHP's implementation, measuring display width instead of bytes and keeping ANSI sequences.
func wordwrap(text string, width int) string {
	var (
		units  []string
		widths []int
		state  byte
	)
	for text != "" {
		seq, w, n, newState := ansi.DecodeSequence(text, state, nil)
		units, widths, state, text = append(units, seq), append(widths, w), newState, text[n:]
	}
	pos := make([]int, len(units)+1)
	for i, w := range widths {
		pos[i+1] = pos[i] + w
	}

	var b strings.Builder
	write := func(from, to int, lineBreak bool) {
		b.WriteString(strings.Join(units[from:to], ""))
		if lineBreak {
			b.WriteString("\n")
		}
	}
	lastStart, lastSpace := 0, 0
	for cur, unit := range units {
		// The line is full, as in PHP, or the current unit would overflow it (if it is wide).
		lineWidth := pos[cur] - pos[lastStart]
		over := lineWidth >= width || lineWidth+widths[cur] > width
		switch {
		case unit == "\n" && cur+1 < len(units):
			// Keep existing line breaks.
			write(lastStart, cur+1, false)
			lastStart, lastSpace = cur+1, cur+1
		case unit == " ":
			// Break at a space at the line boundary.
			if lineWidth >= width {
				write(lastStart, cur, true)
				lastStart = cur + 1
			}
			lastSpace = cur
		case over && lastStart >= lastSpace && cur > lastStart:
			// Cut a word that is too long.
			write(lastStart, cur, true)
			lastStart, lastSpace = cur, cur
		case over && lastStart < lastSpace:
			// Break at the last space.
			write(lastStart, lastSpace, true)
			lastStart, lastSpace = lastSpace+1, lastSpace+1
		}
	}
	write(lastStart, len(units), false)
	return b.String()
}

var (
	sgrRegex       = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)
	hyperlinkRegex = regexp.MustCompile(`\x1b\]8;[^;\x07\x1b]*;([^\x07\x1b]*)(?:\x07|\x1b\\)`)
)

const hyperlinkClose = "\x1b]8;;\x1b\\"

// isolateStyles closes any ANSI style (SGR) or hyperlink (OSC 8) still open at the end of each line,
// and re-opens it on the next line. This stops them from leaking into the table's borders and other cells.
func isolateStyles(lines []string) []string {
	style, link := "", ""
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = style + link + line
		for _, m := range sgrRegex.FindAllStringSubmatch(line, -1) {
			if sgrResets(m[1]) {
				style = ""
			}
			if m[1] != "" && m[1] != "0" {
				style += m[0]
			}
		}
		for _, m := range hyperlinkRegex.FindAllStringSubmatch(line, -1) {
			link = ""
			if m[1] != "" {
				link = m[0]
			}
		}
		if link != "" {
			out[i] += hyperlinkClose
		}
		if style != "" {
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
