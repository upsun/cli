<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests;

use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\FileLock;

trait LockHolderTrait
{
    /**
     * Starts a process which acquires a lock, holds it, and then exits.
     *
     * @return resource
     */
    private function startLockHolder(string $homeDir, string $lockName, int $holdSeconds = 60)
    {
        $script = \sprintf(
            'require %s; $l = new %s(new %s(["PLATFORMSH_CLI_HOME" => %s])); $l->acquireOrWait(%s); echo "locked\n"; sleep(%d);',
            \var_export(\dirname(__DIR__) . '/vendor/autoload.php', true),
            FileLock::class,
            Config::class,
            \var_export($homeDir, true),
            \var_export($lockName, true),
            $holdSeconds,
        );
        $process = \proc_open([\PHP_BINARY, '-r', $script], [1 => ['pipe', 'w']], $pipes);
        $this->assertIsResource($process);
        $this->assertSame("locked\n", \fgets($pipes[1]));

        return $process;
    }
}
