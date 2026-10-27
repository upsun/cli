<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Util;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Util\TimezoneUtil;

class TimezoneUtilTest extends TestCase
{
    private string $originalSetting;
    private string|bool $originalEnv;

    public function setUp(): void
    {
        // Reset to PHP defaults.
        $this->originalSetting = date_default_timezone_get();
        $this->originalEnv = getenv('TZ');
        date_default_timezone_set('UTC');
        putenv('TZ=');
    }

    public function tearDown(): void
    {
        // Reset to original settings.
        date_default_timezone_set($this->originalSetting);
        putenv($this->originalEnv !== false ? 'TZ=' . $this->originalEnv : 'TZ');
    }

    public function testGetTimezoneReturnsCurrent(): void
    {
        date_default_timezone_set('Antarctica/Troll');
        $this->assertEquals('Antarctica/Troll', TimezoneUtil::getTimezone());
    }

    public function testGetTimezoneReturnsTzVariable(): void
    {
        foreach (['Pacific/Galapagos', 'America/Port-au-Prince', 'Asia/Ust-Nera', 'Etc/GMT+5'] as $tz) {
            putenv('TZ=' . $tz);
            $this->assertEquals($tz, TimezoneUtil::getTimezone());
        }
    }

    public function testGetTimezoneIgnoresInvalidTzVariable(): void
    {
        foreach (['Europe/Pari', ':zone.tab'] as $tz) {
            putenv('TZ=' . $tz);
            $this->assertContains(TimezoneUtil::getTimezone(), \DateTimeZone::listIdentifiers(\DateTimeZone::ALL_WITH_BC), "TZ=$tz");
        }
    }

    public function testGetTimezoneReturnsSomething(): void
    {
        $this->assertNotEmpty(TimezoneUtil::getTimezone());
    }

    public function testConvertTz(): void
    {
        $method = new \ReflectionMethod(TimezoneUtil::class, 'convertTz');
        $zoneinfoDir = dirname(__DIR__) . DIRECTORY_SEPARATOR . 'data' . DIRECTORY_SEPARATOR . 'zoneinfo';
        $cases = [
            'UTC' => 'UTC',
            'GMT' => 'GMT',
            'AEST' => 'AEST',
            'UTC0' => 'UTC',
            'UTC+0' => 'UTC',
            'UTC-0' => 'UTC',
            'AEST+0' => 'AEST',
            'UTC+1' => false,
            'name' => 'name',
            'x' => false, // the name must be "three or more characters long"
            ':Antarctica/Troll' => 'Antarctica/Troll',
            ":$zoneinfoDir/Antarctica/Troll" => 'Antarctica/Troll',
            ':Antarctica/Nowhere' => false,
            ':/etc/Antarctica/Troll' => false,
            ':' => false,
        ];
        foreach ($cases as $input => $expected) {
            $this->assertEquals($expected, $method->invoke(null, $input, $zoneinfoDir), "converting \"$input\"");
        }
    }

    public function testZoneFromLink(): void
    {
        $method = new \ReflectionMethod(TimezoneUtil::class, 'zoneFromLink');
        $cases = [
            '/usr/share/zoneinfo/Europe/Paris' => 'Europe/Paris',
            '../usr/share/zoneinfo/Europe/Paris' => 'Europe/Paris',
            './../usr/share//zoneinfo/America/New_York' => 'America/New_York',
            '/var/db/timezone/zoneinfo/Asia/Tokyo' => 'Asia/Tokyo',
            'zoneinfo/Europe/Paris' => false,
            '/opt/zoneinfo/Europe/Paris' => false,
        ];
        foreach ($cases as $target => $expected) {
            $this->assertEquals($expected, $method->invoke(null, '/etc/localtime', $target), "resolving \"$target\"");
        }
    }
}
