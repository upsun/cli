<?php

declare(strict_types=1);

namespace Platformsh\Cli\Util;

class TimezoneUtil
{
    /**
     * Get the timezone intended by the user.
     *
     * The timezone is detected with the following priorities:
     *
     * 1. A value previously set via date_default_timezone_set() or the ini
     *    setting 'date.timezone', which can only be known if it is not the
     *    default, UTC.
     * 2. The value of the TZ environment variable, if set to a known timezone.
     * 3. A best guess at the system timezone, if known: see self::detectSystemTimezone().
     * 4. Default to UTC.
     *
     * @return string
     */
    public static function getTimezone(): string
    {
        // Suppress the PHP warning, "It is not safe to rely on the system's
        // timezone settings".
        $currentTz = @date_default_timezone_get();
        if ($currentTz !== 'UTC') {
            return $currentTz;
        }

        if ($tz = getenv('TZ')) {
            // A zone name such as America/Port-au-Prince does not fit the POSIX format that convertTz() parses.
            if (self::isKnown($tz)) {
                return $tz;
            }
            $converted = self::convertTz($tz);
            if ($converted !== false && self::isKnown($converted)) {
                return $converted;
            }
        }

        if (($systemTz = self::detectSystemTimezone()) && self::isKnown($systemTz)) {
            return $systemTz;
        }

        return $currentTz;
    }

    /**
     * Checks that PHP accepts a timezone name, e.g. for date_default_timezone_set().
     */
    private static function isKnown(string $tz): bool
    {
        return \in_array($tz, \DateTimeZone::listIdentifiers(\DateTimeZone::ALL_WITH_BC), true);
    }

    /**
     * Converts the TZ environment variable format to the PHP format.
     *
     * Variable values with non-zero offsets are not supported.
     *
     * @link https://www.gnu.org/software/libc/manual/html_node/TZ-Variable.html
     * @see date_default_timezone_set()
     *
     * @param string $tz
     * @param string $zoneinfoDir
     *
     * @return string|false
     *   A string or false if the value cannot be read or is invalid.
     */
    private static function convertTz(string $tz, string $zoneinfoDir = '/usr/share/zoneinfo'): string|false
    {
        if (\strlen($tz) > 1 && $tz[0] === ':') {
            // The ":" form names a zoneinfo file, absolute or relative to the zoneinfo directory.
            $name = substr($tz, 1);
            if ($name[0] === '/') {
                if (!str_starts_with($name, $zoneinfoDir . '/')) {
                    return false;
                }
                $name = substr($name, \strlen($zoneinfoDir) + 1);
            }
            return is_file($zoneinfoDir . '/' . $name) ? $name : false;
        }
        if (preg_match('/^([^:0-9,+-]{3,})(0|[+-]0+|$)/i', $tz, $matches) === 1) {
            return $matches[1];
        }
        return false;
    }

    /**
     * Detect the system timezone, restoring functionality from PHP < 5.4.
     *
     * @return string|false
     */
    private static function detectSystemTimezone(): string|false
    {
        // Mac OS X (and older Linuxes): /etc/localtime is a symlink to the
        // timezone in /usr/share/zoneinfo or /var/db/timezone/zoneinfo.
        if (is_link('/etc/localtime') && ($target = readlink('/etc/localtime'))
            && ($zone = self::zoneFromLink('/etc/localtime', $target))) {
            return $zone;
        }

        // Ubuntu and Debian.
        if (file_exists('/etc/timezone')) {
            $data = file_get_contents('/etc/timezone');
            if ($data !== false) {
                return trim($data);
            }
        }

        // RHEL and CentOS.
        if (file_exists('/etc/sysconfig/clock')) {
            $data = parse_ini_file('/etc/sysconfig/clock');
            if (!empty($data['ZONE'])) {
                return trim((string) $data['ZONE']);
            }
        }

        return false;
    }

    /**
     * Finds the timezone name from the target of a symlink to a zoneinfo file.
     */
    private static function zoneFromLink(string $link, string $target): string|false
    {
        if ($target[0] !== '/') {
            $target = \dirname($link) . '/' . $target;
        }
        $parts = [];
        foreach (explode('/', $target) as $part) {
            if ($part === '..') {
                array_pop($parts);
            } elseif ($part !== '' && $part !== '.') {
                $parts[] = $part;
            }
        }
        $path = '/' . implode('/', $parts);
        foreach (['/usr/share/zoneinfo/', '/var/db/timezone/zoneinfo/'] as $prefix) {
            if (str_starts_with($path, $prefix)) {
                return substr($path, \strlen($prefix));
            }
        }
        return false;
    }
}
