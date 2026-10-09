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

    public function testIsWslInteropEnabled(): void
    {
        $dir = sys_get_temp_dir() . '/binfmt-' . bin2hex(random_bytes(4));
        mkdir($dir);
        try {
            $this->assertFalse(OsUtil::isWslInteropEnabled($dir));
            file_put_contents($dir . '/WSLInterop', "disabled\ninterpreter /init\n");
            $this->assertFalse(OsUtil::isWslInteropEnabled($dir));
            file_put_contents($dir . '/WSLInterop-late', "enabled\ninterpreter /init\n");
            $this->assertTrue(OsUtil::isWslInteropEnabled($dir));
        } finally {
            foreach (glob($dir . '/*') ?: [] as $file) {
                unlink($file);
            }
            rmdir($dir);
        }
    }
}
