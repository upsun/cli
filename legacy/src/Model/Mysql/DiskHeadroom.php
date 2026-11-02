<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model\Mysql;

use Platformsh\Cli\Command\Db\DbMysqlOptimizeCommand;
use Platformsh\Cli\Service\Observability;

/**
 * The disk and /tmp usage of a database service, from the metrics API.
 */
readonly class DiskHeadroom
{
    public function __construct(
        public float $diskUsed,
        public float $diskLimit,
        public ?float $tmpUsed = null,
        public ?float $tmpLimit = null,
    ) {}

    /**
     * Reads the usage from a service's metrics data point.
     *
     * @param array<mixed> $point
     */
    public static function fromMetrics(array $point): ?self
    {
        $value = function (string $mountpoint, string $metric, string $agg) use ($point): ?float {
            $v = Observability::nested($point, 'mountpoints', $mountpoint, $metric, $agg);
            return is_numeric($v) ? (float) $v : null;
        };
        $diskUsed = $value('/mnt', 'disk_used', 'avg');
        $diskLimit = $value('/mnt', 'disk_limit', 'max');
        if ($diskUsed === null || !$diskLimit) {
            return null;
        }
        $tmpUsed = $value('/tmp', 'disk_used', 'avg');
        $tmpLimit = $value('/tmp', 'disk_limit', 'max');
        if ($tmpUsed === null || !$tmpLimit) {
            $tmpUsed = $tmpLimit = null;
        }

        return new self($diskUsed, $diskLimit, $tmpUsed, $tmpLimit);
    }

    public function diskFree(): float
    {
        return max(0, $this->diskLimit - $this->diskUsed);
    }

    public function hasTmp(): bool
    {
        return $this->tmpLimit !== null;
    }

    public function tmpFree(): ?float
    {
        return $this->tmpLimit !== null ? max(0, $this->tmpLimit - (float) $this->tmpUsed) : null;
    }

    /**
     * Returns whether the disk has room for a rebuild, keeping a reserve free.
     */
    public function diskFits(int $needed): bool
    {
        return self::fits($this->diskUsed, $this->diskLimit, $needed);
    }

    /**
     * Returns whether /tmp has room for a rebuild, or null if its usage is unknown.
     */
    public function tmpFits(int $needed): ?bool
    {
        if ($this->tmpLimit === null) {
            return null;
        }

        return self::fits((float) $this->tmpUsed, $this->tmpLimit, $needed);
    }

    private static function fits(float $used, float $limit, int $needed): bool
    {
        return $used + $needed <= $limit * (100 - DbMysqlOptimizeCommand::RESERVE_PERCENT) / 100;
    }
}
