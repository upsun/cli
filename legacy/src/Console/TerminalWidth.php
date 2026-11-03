<?php

declare(strict_types=1);

namespace Platformsh\Cli\Console;

use Symfony\Component\Console\Terminal;

/**
 * Finds the current width of the terminal, so that output adapts if it is resized while a command runs.
 *
 * Symfony's Terminal caches the width, and its Application exports it as COLUMNS on startup.
 */
class TerminalWidth
{
    private static ?bool $columnsFromUser = null;

    /**
     * Records whether COLUMNS was set by the user. Call this before Symfony sets it.
     *
     * Only the first call counts, as nested applications (e.g. in the multi command) run after it is set.
     */
    public static function captureEnv(): void
    {
        self::$columnsFromUser ??= getenv('COLUMNS') !== false;
    }

    public static function get(): int
    {
        if (self::$columnsFromUser === false && ($width = static::readWidth()) !== null) {
            return $width;
        }

        return (new Terminal())->getWidth();
    }

    /**
     * Parses the output of `stty size` ("rows columns").
     */
    public static function parseSttySize(string $output): ?int
    {
        if (preg_match('/^\d+ (\d+)$/', trim($output), $matches) && (int) $matches[1] > 0) {
            return (int) $matches[1];
        }

        return null;
    }

    protected static function readWidth(): ?int
    {
        // Symfony's Terminal uses other methods on Windows.
        if (DIRECTORY_SEPARATOR === '\\' || !\function_exists('proc_open')) {
            return null;
        }
        // The process inherits stdin, which stty reads the size from.
        $process = @proc_open(['stty', 'size'], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
        if (!$process) {
            return null;
        }
        $output = (string) stream_get_contents($pipes[1]);
        fclose($pipes[1]);
        fclose($pipes[2]);
        if (proc_close($process) !== 0) {
            return null;
        }

        return self::parseSttySize($output);
    }
}
