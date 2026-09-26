<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Util;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Util\SshConfigInspector;

class SshConfigInspectorTest extends TestCase
{
    public function testIncludesPath(): void
    {
        $wildcards = ['*.platform.sh', '*.upsun.com'];
        $include = '/home/user/.upsun-cli/ssh/*.config';
        $cases = [
            'exact suggested snippet' => [
                "Host *.platform.sh *.upsun.com\n  Include $include\nHost *\n",
                true,
            ],
            'extra options in the block' => [
                "# BEGIN: Upsun certificate configuration\n"
                . "Host *.platform.sh *.upsun.com\n"
                . "  StrictHostKeyChecking no\n"
                . "  UserKnownHostsFile /dev/null\n"
                . "  LogLevel ERROR\n"
                . "  Include $include\n"
                . "# END: Upsun certificate configuration\n",
                true,
            ],
            'different indentation, case, and pattern order' => [
                "host *.UPSUN.com *.platform.sh\n\tinclude $include\n",
                true,
            ],
            'keyword with equals sign' => [
                "Host=*.platform.sh *.upsun.com\nInclude=$include\n",
                true,
            ],
            'include among several paths' => [
                "Host *.platform.sh *.upsun.com\n  Include ~/.ssh/other.config $include\n",
                true,
            ],
            'tilde expansion' => [
                "Host *.platform.sh *.upsun.com\n  Include ~/.upsun-cli/ssh/*.config\n",
                true,
            ],
            'quoted path' => [
                "Host *.platform.sh *.upsun.com\n  Include \"$include\"\n",
                true,
            ],
            'top-level include' => [
                "Include $include\n\nHost github.com\n  User git\n",
                true,
            ],
            'match all block' => [
                "Match all\n  Include $include\n",
                true,
            ],
            'catch-all host block' => [
                "Host *\n  Include $include\n",
                true,
            ],
            'Windows line endings' => [
                "Host *.platform.sh *.upsun.com\r\n  Include $include\r\n",
                true,
            ],
            'empty file' => [
                '',
                false,
            ],
            'commented out' => [
                "# Host *.platform.sh *.upsun.com\n#  Include $include\n",
                false,
            ],
            'missing one wildcard' => [
                "Host *.platform.sh\n  Include $include\n",
                false,
            ],
            'different include path' => [
                "Host *.platform.sh *.upsun.com\n  Include /home/user/.platformsh/ssh/*.config\n",
                false,
            ],
            'include in an unrelated host block' => [
                "Host *.platform.sh *.upsun.com\n  User foo\nHost github.com\n  Include $include\n",
                false,
            ],
            'include in a match block' => [
                "Match host *.platform.sh\n  Include $include\n",
                false,
            ],
            'negated pattern' => [
                "Host *.platform.sh *.upsun.com !foo.upsun.com\n  Include $include\n",
                false,
            ],
            'include path as a prefix only' => [
                "Host *.platform.sh *.upsun.com\n  Include {$include}.bak\n",
                false,
            ],
        ];
        foreach ($cases as $name => [$contents, $expected]) {
            $this->assertSame($expected, SshConfigInspector::includesPath($contents, $wildcards, [$include], '/home/user'), $name);
        }
    }

    public function testQuotedIncludePathWithSpaces(): void
    {
        $include = '"/home/some user/.upsun-cli/ssh/*.config"';
        $contents = "Host *.example.com\n  Include \"/home/some user/.upsun-cli/ssh/*.config\"\n";
        $this->assertTrue(SshConfigInspector::includesPath($contents, ['*.example.com'], [$include]));
        $this->assertFalse(SshConfigInspector::includesPath($contents, ['*.example.com'], ['/home/other/.upsun-cli/ssh/*.config']));
    }

    public function testEmptyInputs(): void
    {
        $this->assertFalse(SshConfigInspector::includesPath("Include /foo\n", [], ['/foo']));
        $this->assertFalse(SshConfigInspector::includesPath("Include /foo\n", ['*.example.com'], []));
    }
}
