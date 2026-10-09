<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Util;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Util\OsUtil;

class OsUtilTest extends TestCase
{
    public function testEscapePosixShellArg(): void
    {
        $this->assertEquals(
            "'This isn'\\''t an argument!'",
            OsUtil::escapePosixShellArg("This isn't an argument!"),
        );
        $this->assertEquals(
            "'Yes it is'",
            OsUtil::escapePosixShellArg("Yes it is"),
        );
        $this->assertEquals(
            "'No it isn'\\''t'",
            OsUtil::escapePosixShellArg("No it isn't"),
        );
    }

    public function testIsWsl(): void
    {
        if (!OsUtil::isLinux()) {
            $this->markTestSkipped('WSL is Linux');
        }
        if (stripos((string) @file_get_contents('/proc/sys/kernel/osrelease'), 'microsoft') !== false) {
            $this->markTestSkipped('running in WSL');
        }
        $distro = getenv('WSL_DISTRO_NAME');
        $interop = getenv('WSL_INTEROP');
        try {
            putenv('WSL_DISTRO_NAME');
            putenv('WSL_INTEROP');
            $this->assertFalse(OsUtil::isWsl());
            putenv('WSL_DISTRO_NAME=Ubuntu');
            $this->assertTrue(OsUtil::isWsl());
        } finally {
            putenv($distro === false ? 'WSL_DISTRO_NAME' : "WSL_DISTRO_NAME=$distro");
            putenv($interop === false ? 'WSL_INTEROP' : "WSL_INTEROP=$interop");
        }
    }
}
