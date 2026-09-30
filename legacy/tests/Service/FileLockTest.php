<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\FileLock;
use Platformsh\Cli\Tests\HasTempDirTrait;

class FileLockTest extends TestCase
{
    use HasTempDirTrait;

    private Config $config;

    public function setUp(): void
    {
        $this->tempDirSetUp();
        $this->config = new Config(['PLATFORMSH_CLI_HOME' => (string) $this->tempDir]);
    }

    public function testWaitsForAHeldLock(): void
    {
        $holder = new FileLock($this->config);
        $this->assertNull($holder->acquireOrWait('test'));

        $this->assertSame('checked', (new FileLock($this->config))->acquireOrWait('test', null, fn(): string => 'checked'));

        $holder->release('test');
        $this->assertNull((new FileLock($this->config))->acquireOrWait('test', null, fn(): string => 'checked'));
    }

    public function testLockIsFreedWhenTheHolderIsKilled(): void
    {
        $holder = $this->startHolderProcess('test');
        $this->assertSame('checked', (new FileLock($this->config))->acquireOrWait('test', null, fn(): string => 'checked'));

        \proc_terminate($holder, 9);
        \proc_close($holder);

        $this->assertNull((new FileLock($this->config))->acquireOrWait('test', null, fn(): string => 'checked'));
    }

    /**
     * Starts a process which acquires a lock and then sleeps.
     *
     * @return resource
     */
    private function startHolderProcess(string $lockName)
    {
        $script = \sprintf(
            'require %s; $l = new %s(new %s(["PLATFORMSH_CLI_HOME" => %s])); $l->acquireOrWait(%s); echo "locked\n"; sleep(60);',
            \var_export(\dirname(__DIR__, 2) . '/vendor/autoload.php', true),
            FileLock::class,
            Config::class,
            \var_export($this->tempDir, true),
            \var_export($lockName, true),
        );
        $process = \proc_open([\PHP_BINARY, '-r', $script], [1 => ['pipe', 'w']], $pipes);
        $this->assertIsResource($process);
        $this->assertSame("locked\n", \fgets($pipes[1]));

        return $process;
    }
}
