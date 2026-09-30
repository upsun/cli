<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\FileLock;
use Platformsh\Cli\Tests\HasTempDirTrait;
use Platformsh\Cli\Tests\LockHolderTrait;

class FileLockTest extends TestCase
{
    use HasTempDirTrait;
    use LockHolderTrait;

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

    public function testIsHeldOnlyWhenAcquired(): void
    {
        $holder = new FileLock($this->config);
        $holder->acquireOrWait('test');
        $this->assertTrue($holder->isHeld('test'));

        $waiter = new FileLock($this->config, 1);
        $this->assertNull($waiter->acquireOrWait('test'));
        $this->assertFalse($waiter->isHeld('test'));

        $holder->release('test');
        $this->assertFalse($holder->isHeld('test'));
    }

    public function testLockIsFreedWhenTheHolderIsKilled(): void
    {
        $holder = $this->startLockHolder((string) $this->tempDir, 'test');
        $this->assertSame('checked', (new FileLock($this->config))->acquireOrWait('test', null, fn(): string => 'checked'));

        \proc_terminate($holder, 9);
        \proc_close($holder);

        $this->assertNull((new FileLock($this->config))->acquireOrWait('test', null, fn(): string => 'checked'));
    }
}
