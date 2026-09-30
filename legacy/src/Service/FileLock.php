<?php

declare(strict_types=1);

namespace Platformsh\Cli\Service;

/**
 * Locks between CLI processes, using OS file locks.
 *
 * The OS releases a lock when its process exits, even if it crashes.
 */
class FileLock
{
    private readonly int $checkIntervalMs;
    private readonly bool $disabled;

    /** @var array<string, resource> */
    private array $locks = [];

    /**
     * @param int $timeLimit The maximum time to wait for a lock, in seconds.
     */
    public function __construct(private readonly Config $config, private readonly int $timeLimit = 30)
    {
        $this->checkIntervalMs = 500;
        $this->disabled = $this->config->getBool('api.disable_locks');
    }

    /**
     * Acquires a lock, or waits for one if it already exists.
     *
     * If the lock is not acquired within the time limit, this returns null
     * without holding the lock.
     *
     * @param string $lockName
     *   A unique name for the lock.
     * @param callable|null $onWait
     *   A function to run when waiting starts.
     * @param callable|null $check
     *   A function to run each time the interval has passed. If it returns a
     *   non-null value, waiting will stop, and the value will be returned
     *   from this method.
     *
     * @return mixed|null
     */
    public function acquireOrWait(string $lockName, ?callable $onWait = null, ?callable $check = null): mixed
    {
        if ($this->disabled || isset($this->locks[$lockName])) {
            return null;
        }
        $handle = $this->open($this->filename($lockName));
        $start = \time();
        $runOnWait = false;
        while (!\flock($handle, LOCK_EX | LOCK_NB)) {
            if (\time() - $start >= $this->timeLimit) {
                \fclose($handle);
                return null;
            }
            if ($onWait !== null && !$runOnWait) {
                $onWait();
                $runOnWait = true;
            }
            \usleep($this->checkIntervalMs * 1000);
            if ($check !== null) {
                $result = $check();
                if ($result !== null) {
                    \fclose($handle);
                    return $result;
                }
            }
        }
        $this->locks[$lockName] = $handle;
        return null;
    }

    /**
     * Checks whether this process holds a lock, or locks are disabled.
     */
    public function isHeld(string $lockName): bool
    {
        return $this->disabled || isset($this->locks[$lockName]);
    }

    /**
     * Releases a lock that was created by acquireOrWait().
     */
    public function release(string $lockName): void
    {
        if (isset($this->locks[$lockName])) {
            \flock($this->locks[$lockName], LOCK_UN);
            \fclose($this->locks[$lockName]);
            unset($this->locks[$lockName]);
        }
    }

    /**
     * Destructor. Releases locks that still exist on exit.
     */
    public function __destruct()
    {
        foreach (\array_keys($this->locks) as $lockName) {
            $this->release($lockName);
        }
    }

    /**
     * Finds the filename for a lock.
     */
    private function filename(string $lockName): string
    {
        return $this->config->getWritableUserDir()
            . DIRECTORY_SEPARATOR . 'locks'
            . DIRECTORY_SEPARATOR
            . preg_replace('/[^\w_-]+/', '-', $lockName)
            . '.lock';
    }

    /**
     * Opens a lock file, creating it if necessary.
     *
     * @return resource
     */
    private function open(string $filename)
    {
        $dir = \dirname($filename);
        if (!\is_dir($dir) && !\mkdir($dir, 0o777, true) && !\is_dir($dir)) {
            throw new \RuntimeException('Failed to create directory: ' . $dir);
        }
        // Mode "c" creates the file without truncating it.
        $handle = \fopen($filename, 'c');
        if (!$handle) {
            throw new \RuntimeException('Failed to open lock file: ' . $filename);
        }
        return $handle;
    }
}
