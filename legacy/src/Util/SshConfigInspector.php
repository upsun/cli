<?php

declare(strict_types=1);

namespace Platformsh\Cli\Util;

/**
 * Inspects the contents of an OpenSSH client configuration file.
 *
 * This is a deliberately small parser: it only needs to answer whether an
 * Include directive already applies to a set of host patterns. See the
 * ssh_config(5) man page for the file format.
 */
class SshConfigInspector
{
    /**
     * Checks whether the config already includes all of the given paths for all of the given host patterns.
     *
     * The include is accepted if it appears:
     *   - at the top level (before any Host or Match block), or in a "Match all" block, or
     *   - in a Host block whose patterns contain every required pattern, or the catch-all "*".
     *
     * Host blocks containing negated patterns ("!example.com") are ignored, as
     * they may exclude hosts that the CLI needs to configure.
     *
     * @param string $contents
     *   The contents of the SSH config file.
     * @param string[] $hostPatterns
     *   The host patterns (wildcards) that the include must apply to, e.g. ['*.example.com'].
     * @param string[] $includePaths
     *   Required Include paths. These may be quoted in the same way as an SSH
     *   config value. Each one must be included, unless a single "~" path
     *   matches: that resolves for every SSH client, via its own home directory.
     * @param string $homeDir
     *   The user's home directory as SSH sees it, used to expand a leading "~"
     *   in Include paths. If empty, "~" paths are never matched.
     */
    public static function includesPath(string $contents, array $hostPatterns, array $includePaths, string $homeDir = ''): bool
    {
        if ($hostPatterns === [] || $includePaths === []) {
            return false;
        }

        $expected = [];
        foreach ($includePaths as $path) {
            foreach (self::splitArgs($path) as $arg) {
                $expected[] = $arg;
            }
        }
        $expected = \array_values(\array_unique($expected));
        $canonicalExpected = \array_map([self::class, 'canonicalPath'], $expected);
        $found = [];
        $required = \array_map('strtolower', $hostPatterns);

        // The scope is null at the top level, or otherwise the list of Host
        // patterns (lowercase) for the current block. A Match block, other
        // than "Match all", is represented as an empty list so that its
        // Include directives are not counted.
        $scope = null;

        foreach (\preg_split('/\r\n|\r|\n/', $contents) ?: [] as $line) {
            $line = \trim($line);
            if ($line === '' || $line[0] === '#') {
                continue;
            }
            if (!\preg_match('/^([A-Za-z]+)(?:\s*=\s*|\s+)(.*)$/', $line, $matches)) {
                continue;
            }
            $keyword = \strtolower($matches[1]);
            $args = self::splitArgs($matches[2]);
            switch ($keyword) {
                case 'host':
                    $scope = \array_map('strtolower', $args);
                    break;

                case 'match':
                    $scope = \count($args) === 1 && \strtolower($args[0]) === 'all' ? null : [];
                    break;

                case 'include':
                    if ($scope !== null && !self::patternsCover($scope, $required)) {
                        break;
                    }
                    foreach ($args as $arg) {
                        if ($arg === '~' || \str_starts_with($arg, '~/')) {
                            if ($homeDir !== '' && \in_array(self::canonicalPath($homeDir . \substr($arg, 1)), $canonicalExpected, true)) {
                                return true;
                            }
                            continue;
                        }
                        $index = \array_search($arg, $expected, true);
                        if ($index !== false) {
                            $found[$index] = true;
                            if (\count($found) === \count($expected)) {
                                return true;
                            }
                        }
                    }
                    break;
            }
        }

        return false;
    }

    /**
     * Checks whether a Host block's patterns cover all of the required patterns.
     *
     * @param string[] $patterns
     * @param string[] $required
     */
    private static function patternsCover(array $patterns, array $required): bool
    {
        foreach ($patterns as $pattern) {
            if (\str_starts_with($pattern, '!')) {
                return false;
            }
        }
        if (\in_array('*', $patterns, true)) {
            return true;
        }
        foreach ($required as $pattern) {
            if (!\in_array($pattern, $patterns, true)) {
                return false;
            }
        }
        return true;
    }

    /**
     * Splits a config value into arguments, honoring double quotes.
     *
     * @return string[]
     */
    private static function splitArgs(string $value): array
    {
        $args = [];
        $current = '';
        $quoted = false;
        $started = false;
        $length = \strlen($value);
        for ($i = 0; $i < $length; $i++) {
            $char = $value[$i];
            if ($char === '"') {
                $quoted = !$quoted;
                $started = true;
                continue;
            }
            if (!$quoted && ($char === ' ' || $char === "\t")) {
                if ($started) {
                    $args[] = $current;
                    $current = '';
                    $started = false;
                }
                continue;
            }
            $current .= $char;
            $started = true;
        }
        if ($started) {
            $args[] = $current;
        }
        return $args;
    }

    /**
     * Converts a path to a canonical form, for comparing Windows paths in their different formats.
     *
     * For example "C:\Users\me/.ssh" and "/c/Users/me/.ssh" are both converted to "/c/Users/me/.ssh".
     */
    private static function canonicalPath(string $path): string
    {
        $path = \str_replace('\\', '/', $path);
        if (\preg_match('#^([A-Za-z]):/#', $path, $matches)) {
            $path = '/' . \strtolower($matches[1]) . \substr($path, 2);
        }
        return (string) \preg_replace('#/+#', '/', $path);
    }
}
