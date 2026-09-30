<?php

declare(strict_types=1);

namespace Platformsh\Cli\Session;

use Platformsh\Client\Session\Storage\File;

/**
 * Session file storage that reads under a shared lock.
 *
 * File::save() truncates and writes the file under an exclusive lock, so an
 * unlocked read can see an empty or partial file.
 */
class FileStorage extends File
{
    /**
     * @return array<mixed>
     */
    public function load(string $sessionId): array
    {
        $filename = $this->getFilename($sessionId);
        if (!\is_readable($filename)) {
            return [];
        }
        $handle = \fopen($filename, 'r');
        if (!$handle) {
            return [];
        }
        try {
            \flock($handle, LOCK_SH);
            $raw = \stream_get_contents($handle);
        } finally {
            \fclose($handle);
        }
        $data = $raw !== false ? \json_decode($raw, true) : null;

        return \is_array($data) ? $data : [];
    }
}
