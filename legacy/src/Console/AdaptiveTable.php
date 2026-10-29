<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use Symfony\Component\Console\Helper\Helper;
use Symfony\Component\Console\Helper\Table;
use Symfony\Component\Console\Helper\TableCell;
use Symfony\Component\Console\Helper\TableSeparator;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Terminal;

/**
 * Extends the Symfony Console Table to make it adaptive to the terminal width.
 */
class AdaptiveTable extends Table
{
    protected int $maxTableWidth;

    // The following 3 properties are copies of the private properties in the
    // parent Table class.
    /** @var array<array<int|string, string|int|float|TableCell>|TableSeparator> */
    protected array $rowsCopy = [];
    /** @var array<array<int|string, string|TableCell>> */
    protected array $headersCopy = [];

    /**
     * AdaptiveTable constructor.
     *
     * @param OutputInterface $outputCopy
     * @param int|null $maxTableWidth
     * @param int $maxUnbrokenWidth
     *   Words up to this width are not broken when wrapping, if the table can fit.
     * @param int $minUnbrokenWidth
     *   Words up to this width are not broken when wrapping, even if the table does not fit.
     */
    public function __construct(protected OutputInterface $outputCopy, ?int $maxTableWidth = null, protected int $maxUnbrokenWidth = 20, protected int $minUnbrokenWidth = 10)
    {
        $this->maxTableWidth = $maxTableWidth !== null
            ? $maxTableWidth
            : (new Terminal())->getWidth();

        parent::__construct($this->outputCopy);
    }

    /**
     * {@inheritdoc}
     *
     * Overrides Table->addRow() so the row content can be accessed.
     *
     * @param TableSeparator|array<int|string, string|TableCell> $row
     */
    public function addRow(TableSeparator|array $row): static
    {
        if ($row instanceof TableSeparator) {
            $this->rowsCopy[] = $row;

            return parent::addRow($row);
        }

        $this->rowsCopy[] = array_values($row);

        return parent::addRow($row);
    }

    /**
     * {@inheritdoc}
     *
     * Overrides Table->setHeaders() so the header content can be accessed.
     *
     * @param array<mixed> $headers
     */
    public function setHeaders(array $headers): static
    {
        $headers = array_values($headers);
        if ($headers && !is_array($headers[0])) {
            $headers = [$headers];
        }

        $this->headersCopy = $headers;

        return parent::setHeaders($headers);
    }

    /**
     * {@inheritdoc}
     *
     * Overrides Table->render(), to adapt all the cells to the table width.
     */
    public function render(): void
    {
        $this->adaptRows();
        parent::render();
    }

    /**
     * Adapt rows based on the terminal width.
     */
    protected function adaptRows(): void
    {
        // Go through all headers and rows, wrapping their cells until each
        // column meets the max column width.
        $maxColumnWidths = $this->getMaxColumnWidths();
        $this->setRows($this->adaptCells($this->rowsCopy, $maxColumnWidths));
    }

    /**
     * Modify table rows, wrapping their cells' content to the max column width.
     *
     * @param array<array<int|string, string|int|float|TableCell>|TableSeparator> $rows
     * @param array<int|string, int> $maxColumnWidths
     *
     * @return array<array<int|string, string|int|float|TableCell>|TableSeparator>
     */
    protected function adaptCells(array $rows, array $maxColumnWidths): array
    {
        foreach ($rows as &$row) {
            if ($row instanceof TableSeparator) {
                continue;
            }
            foreach ($row as $column => &$cell) {
                $contents = (string) $cell;
                // Replace Windows line endings, because Symfony's buildTableRows() does not respect them.
                if (str_contains($contents, "\r\n")) {
                    $contents = \str_replace("\r\n", "\n", $contents);
                    if ($cell instanceof AdaptiveTableCell) {
                        $cell = $cell->withValue($contents);
                    } elseif (\is_string($cell)) {
                        $cell = $contents;
                    }
                }
                $cellWidth = $this->getCellWidth($cell);
                if ($cellWidth <= $maxColumnWidths[$column]) {
                    continue;
                }
                $wrapped = $this->wrapCell($contents, $maxColumnWidths[$column]);
                if ($cell instanceof TableCell) {
                    $cell = new TableCell($wrapped, [
                        'colspan' => $cell->getColspan(),
                        'rowspan' => $cell->getRowspan(),
                    ]);
                } elseif (is_string($cell)) {
                    $cell = $wrapped;
                }
            }
        }

        return $rows;
    }

    /**
     * Word-wraps the contents of a cell, so that they fit inside a max width.
     */
    private function wrapCell(string $contents, int $width): string
    {
        // Account for left-indented cells.
        if (str_starts_with($contents, ' ')) {
            $trimmed = ltrim($contents, ' ');
            $indentAmount = Helper::width($contents) - Helper::width($trimmed);
            $indent = str_repeat(' ', $indentAmount);

            return preg_replace('/^/m', $indent, $this->wrapWithDecoration($trimmed, max($width - $indentAmount, 1)));
        }

        return $this->wrapWithDecoration($contents, $width);
    }

    /**
     * Word-wraps the contents of a cell, accounting for decoration.
     */
    public function wrapWithDecoration(string $formattedText, int $maxLength): string
    {
        $plainText = Helper::removeDecoration($this->outputCopy->getFormatter(), $formattedText);
        if ($plainText === $formattedText) {
            return $this->wordwrap($plainText, $maxLength);
        }

        // Find all open and closing tags in the formatted text, with their
        // offsets, and build a plain text string out of the rest.
        $tagRegex = '[a-zA-Z][a-zA-Z0-9,_=;-]*+';
        preg_match_all('#</?(?:' . $tagRegex . ')?>#', $formattedText, $matches, PREG_OFFSET_CAPTURE);
        $plainText = '';
        $tagChunks = [];
        $lastTagClose = 0;
        foreach ($matches[0] as $match) {
            [$tagChunk, $tagOffset] = $match;
            if (substr($formattedText, $tagOffset - 1, 1) === '\\') {
                continue;
            }
            $plainText .= substr($formattedText, $lastTagClose, $tagOffset - $lastTagClose);
            $tagChunks[$tagOffset] = $tagChunk;
            $lastTagClose = $tagOffset + strlen($tagChunk);
        }
        $plainText .= substr($formattedText, $lastTagClose);

        // Wrap the plain text, keeping track of the removed characters in each
        // line (caused by trimming).
        $remaining = $plainText;
        $lines = [];
        $removedCharacters = [];
        while ($remaining !== '') {
            if (($break = $this->findBreak($remaining, $maxLength)) !== null) {
                $line = substr($remaining, 0, $break);
                $remaining = substr($remaining, $break);
            } else {
                $line = $remaining;
                $remaining = '';
            }
            $lineTrimmed = trim($line);
            $removedCharacters[] = strlen($line) - strlen($lineTrimmed);
            $lines[] = $lineTrimmed;
        }

        // Interpolate the tags back into the wrapped text.
        $remainingTagChunks = $tagChunks;
        $lineOffset = 0;
        foreach ($lines as $lineNumber => &$line) {
            $lineLength = strlen($line) + $removedCharacters[$lineNumber];
            foreach ($remainingTagChunks as $tagOffset => $tagChunk) {
                // Prefer putting opening tags at the beginning of a line, not
                // the end.
                if ($tagChunk[1] !== '/' && $tagOffset === $lineOffset + $lineLength) {
                    continue;
                }
                if ($tagOffset >= $lineOffset && $tagOffset <= $lineOffset + $lineLength) {
                    $insertPosition = $tagOffset - $lineOffset;
                    $line = substr($line, 0, $insertPosition) . $tagChunk . substr($line, $insertPosition);
                    $lineLength += strlen($tagChunk);
                    unset($remainingTagChunks[$tagOffset]);
                }
            }
            $lineOffset += $lineLength;
        }

        $wrapped = implode("\n", $lines) . implode('', $remainingTagChunks);

        // Ensure that tags are closed at the end of each line and re-opened at
        // the beginning of the next one.
        $wrapped = preg_replace_callback('@(<' . $tagRegex . '>)(((?!(?<!\\\)</).)+)@s', fn(array $matches) => $matches[1] . str_replace("\n", "</>\n" . $matches[1], $matches[2]), $wrapped);

        return $wrapped;
    }

    /**
     * Wraps text like wordwrap($text, $width, "\n", true), by display width instead of bytes.
     */
    private function wordwrap(string $text, int $width): string
    {
        $lines = [];
        $line = '';
        $lineWidth = 0;
        // The byte offset and preceding width of the last space that is not at the start of the line.
        $space = null;
        foreach ($this->splitCharacters($text) as $char) {
            if ($char === "\n" || ($char === ' ' && $lineWidth >= $width)) {
                $lines[] = $line;
                [$line, $lineWidth, $space] = ['', 0, null];
                continue;
            }
            $charWidth = Helper::width($char);
            while ($line !== '' && $lineWidth + $charWidth > $width) {
                if ($space !== null) {
                    [$offset, $widthBefore] = $space;
                    $lines[] = substr($line, 0, $offset);
                    $line = substr($line, $offset + 1);
                    $lineWidth -= $widthBefore + 1;
                    $space = null;
                } else {
                    $lines[] = $line;
                    [$line, $lineWidth] = ['', 0];
                }
            }
            if ($char === ' ' && $line !== '') {
                $space = [\strlen($line), $lineWidth];
            }
            $line .= $char;
            $lineWidth += $charWidth;
        }
        $lines[] = $line;

        return implode("\n", $lines);
    }

    /**
     * Finds where to break a line of text so that it fits a display width.
     *
     * Breaks after the last space that fits, or else cuts the text at the
     * width. An escaped tag opening, "\<", counts as one character.
     *
     * @return int|null
     *   The byte offset where the next line starts, or null if the text fits.
     */
    private function findBreak(string $text, int $maxWidth): ?int
    {
        $width = 0;
        $offset = 0;
        $space = null;
        foreach ($this->splitCharacters($text) as $char) {
            if ($char === ' ') {
                $space = $offset;
            }
            $charWidth = $char === '\\<' ? 1 : Helper::width($char);
            if ($width + $charWidth > $maxWidth) {
                // Cut after at least one character.
                return $space !== null ? $space + 1 : ($offset ?: \strlen($char));
            }
            $width += $charWidth;
            $offset += \strlen($char);
        }

        return null;
    }

    /**
     * Splits text into characters, keeping an escaped tag opening, "\<", as one.
     *
     * @return string[]
     */
    private function splitCharacters(string $text): array
    {
        // Invalid UTF-8 is split into bytes.
        return preg_match_all('/\\\\<|./us', $text, $matches) !== false ? $matches[0] : str_split($text);
    }

    /**
     * Finds the maximum width of each column's content, so that the table fits into the maximum table width.
     *
     * This is similar to a web browser's automatic table layout. Each column
     * has a maximum width (its widest cell) and a minimum width (its longest
     * word, up to $maxUnbrokenWidth, or less if needed to fit). If the maximum widths do not fit, each
     * column gets its minimum width plus a share of the remaining space, in
     * proportion to the difference between its maximum and minimum widths.
     *
     * @return array<int|string, int>
     *   An array of the maximum column widths that fit into the table width,
     *   indexed by the column's key in the table's rows (a name or number).
     */
    protected function getMaxColumnWidths(): array
    {
        // Loop through the table rows and headers, building arrays of the
        // maximum column widths, the widths of cells that cannot wrap, and the
        // widths of the longest words. In the same loop, build a count of the
        // number of columns.
        $maxWidths = [];
        $fixedWidths = [];
        $wordWidths = [];
        $columnCounts = [0];
        foreach (array_merge($this->rowsCopy, $this->headersCopy) as $rowNum => $row) {
            if ($row instanceof TableSeparator) {
                continue;
            }
            $columnCount = 0;
            foreach ($row as $column => $cell) {
                $columnCount += $cell instanceof TableCell ? $cell->getColspan() : 1;

                // The maximum column width is the width of the widest cell.
                $cellWidth = (int) ceil($this->getCellWidth($cell));
                $maxWidths[$column] = max($maxWidths[$column] ?? 0, $cellWidth);

                // Non-wrapping cells and table headers are never wrapped.
                // Otherwise, track the width of the longest word.
                if (($cell instanceof AdaptiveTableCell && !$cell->canWrap()) || !isset($this->rowsCopy[$rowNum])) {
                    $fixedWidths[$column] = max($fixedWidths[$column] ?? 0, $cellWidth);
                } else {
                    // A cell at its full width is not wrapped, so its minimum is no more than that.
                    $wordWidths[$column] = max($wordWidths[$column] ?? 0, min((int) ceil($this->getLongestWordWidth($cell)), $cellWidth));
                }
            }
            $columnCounts[] = $columnCount;
        }

        // Find the number of columns in the table. This uses the same process
        // as the parent private method Table->calculateNumberOfColumns().
        $columnCount = max($columnCounts);
        $available = (int) $this->getMaxContentWidth($columnCount);

        $maxTotal = array_sum($maxWidths);
        if ($maxTotal <= $available) {
            return $maxWidths;
        }

        // The minimum column width is the width of the longest word, capped
        // at $maxUnbrokenWidth. The cap is reduced, down to
        // $minUnbrokenWidth, until the minimum widths fit.
        for ($cap = $this->maxUnbrokenWidth; ; $cap--) {
            $minWidths = [];
            foreach ($maxWidths as $column => $maxWidth) {
                $minWidths[$column] = max($fixedWidths[$column] ?? 0, min($wordWidths[$column] ?? 0, $cap));
            }
            $minTotal = array_sum($minWidths);
            if ($minTotal <= $available || $cap <= $this->minUnbrokenWidth) {
                break;
            }
        }
        if ($minTotal >= $available) {
            return $minWidths;
        }

        // Share the extra space in proportion to each column's flexibility
        // (its maximum minus its minimum width). Integer arithmetic is used,
        // and the remainder is given to the columns with the largest
        // fractional parts.
        $extra = $available - $minTotal;
        $flexTotal = $maxTotal - $minTotal;
        $widths = [];
        $remainders = [];
        foreach ($maxWidths as $column => $maxWidth) {
            $share = ($maxWidth - $minWidths[$column]) * $extra;
            $widths[$column] = $minWidths[$column] + intdiv($share, $flexTotal);
            $remainders[$column] = $share % $flexTotal;
        }
        $leftover = $available - array_sum($widths);
        // Sorting is stable, so ties are broken by column order.
        $columns = array_keys($remainders);
        usort($columns, fn($a, $b): int => $remainders[$b] <=> $remainders[$a]);
        foreach (array_slice($columns, 0, $leftover) as $column) {
            $widths[$column]++;
        }

        return $widths;
    }

    /**
     * Find the maximum content width (excluding decoration) for each table row.
     *
     * @param int $columnCount
     *   The number of columns in the table.
     *
     * @return int|float
     *   The maximum table width, minus the width taken up by decoration.
     */
    protected function getMaxContentWidth(int $columnCount): int|float
    {
        $style = $this->getStyle();
        $verticalBorderQuantity = $columnCount + 1;
        $paddingQuantity = $columnCount * 2;

        return $this->maxTableWidth
            - $verticalBorderQuantity * strlen((string) $style->getBorderChars()[3])
            - $paddingQuantity * strlen($style->getPaddingChar());
    }

    /**
     * Get the width of the longest word in a table cell, plus its indentation.
     */
    private function getLongestWordWidth(string|int|float|TableCell $cell): int|float
    {
        $formatter = $this->outputCopy->getFormatter();
        $plain = Helper::removeDecoration($formatter, (string) $cell);
        $width = 0;
        foreach (preg_split('/\s+/', $plain, -1, PREG_SPLIT_NO_EMPTY) ?: [] as $word) {
            $width = max($width, Helper::width($word));
        }
        // Indentation is repeated on each wrapped line.
        $width += strlen($plain) - strlen(ltrim($plain, ' '));
        if ($cell instanceof TableCell && $cell->getColspan() > 1) {
            $width /= $cell->getColspan();
        }

        return $width;
    }

    /**
     * Get the default width of a table cell (the length of its longest line).
     *
     * This is inspired by Table->getCellWidth(), but this also accounts for
     * multi-line cells.
     *
     * @param mixed $cell
     *
     * @return float|int
     */
    private function getCellWidth(mixed $cell): int|float
    {
        $lineWidths = [0];
        $formatter = $this->outputCopy->getFormatter();
        foreach (explode(PHP_EOL, (string) $cell) as $line) {
            $lineWidths[] = Helper::width(Helper::removeDecoration($formatter, $line));
        }
        $cellWidth = max($lineWidths);
        if ($cell instanceof TableCell && $cell->getColspan() > 1) {
            $cellWidth /= $cell->getColspan();
        }

        return $cellWidth;
    }
}
