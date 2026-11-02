<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\GoAuth;
use Platformsh\Cli\Util\OsUtil;
use Symfony\Component\Console\Output\NullOutput;
use Symfony\Component\Filesystem\Filesystem;

/**
 * Tests requests over the wrapper's auth channel, with a fake channel.
 */
class GoAuthTest extends TestCase
{
    private const PREFIX = 'MOCK_CLI_';

    private string $dir;

    /** @var resource|null */
    private $server = null;

    /** The stub set by the bootstrap script, which is restored after each test. */
    private string $stubExecutable;

    public function setUp(): void
    {
        if (OsUtil::isWindows() || !function_exists('posix_getppid')) {
            $this->markTestSkipped('The auth channel is a named pipe on Windows');
        }
        $this->dir = sys_get_temp_dir() . '/go-auth-test-' . bin2hex(random_bytes(4));
        mkdir($this->dir);
        $this->resetChannel();
        $this->stubExecutable = (string) getenv(self::PREFIX . 'WRAPPER_EXECUTABLE');
        // The fallback command prints a token.
        $executable = $this->dir . '/wrapper';
        file_put_contents($executable, "#!/bin/sh\necho '{\"access_token\": \"fallback\"}'\n");
        chmod($executable, 0o755);
        putenv(self::PREFIX . 'WRAPPER_EXECUTABLE=' . $executable);
        putenv(self::PREFIX . 'WRAPPER_AUTH_SOCKET=' . $this->dir . '/s');
        // This test acts as the wrapper's child.
        putenv(self::PREFIX . 'WRAPPER_PID=' . posix_getppid());
    }

    public function tearDown(): void
    {
        if (!isset($this->dir)) {
            return;
        }
        if ($this->server !== null) {
            proc_terminate($this->server);
            proc_close($this->server);
        }
        putenv(self::PREFIX . 'WRAPPER_EXECUTABLE=' . $this->stubExecutable);
        putenv(self::PREFIX . 'WRAPPER_AUTH_SOCKET');
        putenv(self::PREFIX . 'WRAPPER_PID');
        $this->resetChannel();
        (new Filesystem())->remove($this->dir);
    }

    private function resetChannel(): void
    {
        foreach (['channel' => null, 'channelPid' => null, 'wrapperPid' => null, 'tokens' => [], 'status' => []] as $name => $value) {
            (new \ReflectionProperty(GoAuth::class, $name))->setValue(null, $value);
        }
    }

    /**
     * Starts a fake channel, which answers one connection for each response ("close" to answer none).
     */
    private function serve(string ...$responses): void
    {
        $this->server = proc_open(
            [PHP_BINARY, dirname(__DIR__) . '/data/auth-channel-server.php', $this->dir . '/s', ...array_values($responses)],
            [1 => ['pipe', 'w'], 2 => ['pipe', 'w']],
            $pipes,
        ) ?: null;
        $this->assertNotNull($this->server);
        $line = fgets($pipes[1]);
        if ($line !== "ready\n") {
            $this->fail('The fake channel did not start: ' . (string) stream_get_contents($pipes[2]));
        }
    }

    private function goAuth(): GoAuth
    {
        $config = new Config(['MOCK_CLI_HOME' => $this->dir], dirname(__DIR__) . '/data/mock-cli-config.yaml');

        return new GoAuth($config, new NullOutput());
    }

    public function testToken(): void
    {
        $this->serve('{"result": {"access_token": "from-channel"}}');
        $this->assertSame(['access_token' => 'from-channel'], $this->goAuth()->getToken());
    }

    public function testRefusedFallsBack(): void
    {
        $this->serve('{"refused": true}');
        $this->assertSame(['access_token' => 'fallback'], $this->goAuth()->getToken());
        // The channel is no longer used.
        $this->assertSame(['access_token' => 'fallback'], $this->goAuth()->getToken('fallback'));
    }

    public function testNoAnswerFailsWithoutFallingBack(): void
    {
        $this->serve('close', '{"result": {"access_token": "from-channel"}}');
        try {
            $this->goAuth()->getToken();
            $this->fail('An exception must be thrown');
        } catch (\RuntimeException $e) {
            $this->assertStringContainsString('did not answer', $e->getMessage());
        }
        // The channel is still used.
        $this->assertSame(['access_token' => 'from-channel'], $this->goAuth()->getToken());
    }

    public function testConnectionFailureFailsWithoutFallingBack(): void
    {
        $this->expectException(\RuntimeException::class);
        $this->expectExceptionMessage('Failed to connect to the CLI wrapper');
        $this->goAuth()->getToken();
    }

    public function testChannelNotUsedByOtherProcess(): void
    {
        putenv(self::PREFIX . 'WRAPPER_PID=' . (posix_getppid() + 1));
        $this->assertSame(['access_token' => 'fallback'], $this->goAuth()->getToken());
    }
}
