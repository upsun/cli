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
            if ($name = self::knownName($tz)) {
                return $name;
            }
            $converted = self::convertTz($tz);
            if ($converted !== false && ($name = self::knownName($converted))) {
                return $name;
            }
        }

        if (($systemTz = self::detectSystemTimezone()) && ($name = self::knownName($systemTz))) {
            return $name;
        }

        return $currentTz;
    }

    /**
     * Returns a timezone name as PHP lists it, matching case-insensitively as date_default_timezone_set() does.
     */
    private static function knownName(string $tz): string|false
    {
        foreach (\DateTimeZone::listIdentifiers(\DateTimeZone::ALL_WITH_BC) as $identifier) {
            if (strcasecmp($identifier, $tz) === 0) {
                return $identifier;
            }
        }
        return false;
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
     *
     * @return string|false
     *   A string or false if the value cannot be read or is invalid.
     */
    private static function convertTz(string $tz): string|false
    {
        if (\strlen($tz) > 1 && $tz[0] === ':') {
            // The ":" form names a zoneinfo file, relative to a zoneinfo directory or under one. PHP has its own
            // timezone database, so the file need not exist.
            $name = substr($tz, 1);
            return $name[0] === '/' ? self::zoneFromLink('/', $name) : $name;
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
