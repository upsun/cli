<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model\Mysql;

use Platformsh\Cli\Command\Db\DbMysqlOptimizeCommand;

/**
 * An InnoDB table with reclaimable space, from information_schema.
 */
readonly class TableCandidate
{
    /** The ratio of a rebuilt table's data size to the bytes in its rows (for headers and free space in pages). */
    public const PAGE_OVERHEAD = 1.5;

    /** The disk space needed for a rebuild, beyond the estimated size, as a percentage and in bytes (for the file's free extents). */
    public const DISK_MARGIN_PERCENT = 10;
    public const DISK_MARGIN_BYTES = 8 * 1024 * 1024;

    public function __construct(
        public string $schema,
        public string $name,
        public int $dataLength,
        public int $indexLength,
        public int $dataFree,
        /** The estimated number of rows (TABLE_ROWS). */
        public int $rows = 0,
        /** The number of FULLTEXT or SPATIAL indexes, which prevent an online rebuild. */
        public int $offlineIndexes = 0,
        /** The number of other tables reporting the same free space (a sign of a shared tablespace). */
        public int $sameFree = 0,
        /** The average bytes per row, from a sample of rows, or null if unknown. */
        public ?float $avgRowBytes = null,
        /** Whether the sample is the first rows only, rather than spread across the table. */
        public bool $sampledFirstRows = false,
    ) {}

    /**
     * @param string[] $row
     */
    public static function fromRow(array $row): self
    {
        return new self($row[0], $row[1], (int) $row[2], (int) $row[3], (int) $row[4], (int) $row[5], (int) $row[6], (int) $row[7]);
    }

    /**
     * Returns a copy with the result of a sample of rows.
     */
    public function withSample(float $avgRowBytes, bool $firstRows): self
    {
        return new self($this->schema, $this->name, $this->dataLength, $this->indexLength, $this->dataFree, $this->rows, $this->offlineIndexes, $this->sameFree, $avgRowBytes, $firstRows);
    }

    /**
     * The size of the table's data and indexes, excluding free space.
     */
    public function size(): int
    {
        return $this->dataLength + $this->indexLength;
    }

    /**
     * The estimated size of the table's data and indexes after a rebuild, or null if unknown.
     *
     * Secondary indexes are assumed to shrink in the same ratio as the data.
     */
    public function rebuiltSize(): ?int
    {
        if ($this->avgRowBytes === null) {
            return null;
        }
        $data = (int) ceil($this->avgRowBytes * $this->rows * self::PAGE_OVERHEAD);
        $index = $this->dataLength > 0 ? (int) round($this->indexLength * min(1, $data / $this->dataLength)) : $this->indexLength;

        return $data + $index;
    }

    /**
     * The estimated disk space that a rebuild would return.
     *
     * Free extents (DATA_FREE) are always reclaimable. Free space inside
     * pages is only counted if rows were sampled.
     */
    public function reclaimable(): int
    {
        $rebuilt = $this->rebuiltSize();
        if ($rebuilt === null) {
            return $this->dataFree;
        }

        return max($this->dataFree, $this->size() + $this->dataFree - $rebuilt);
    }

    /**
     * The disk space needed for the rebuilt copy of the table.
     */
    public function diskNeeded(): int
    {
        $rebuilt = $this->rebuiltSize();
        if ($rebuilt === null) {
            return $this->size();
        }

        return $rebuilt + intdiv($rebuilt * self::DISK_MARGIN_PERCENT, 100) + self::DISK_MARGIN_BYTES;
    }

    /**
     * The temporary space that may be needed, to sort the secondary indexes.
     *
     * Concurrent writes during the rebuild are also logged to a temporary
     * file (up to innodb_online_alter_log_max_size), which is not counted.
     */
    public function tmpNeeded(): int
    {
        return $this->indexLength;
    }

    public function displayName(): string
    {
        return $this->schema . '.' . $this->name;
    }

    public function quotedName(): string
    {
        return DbMysqlOptimizeCommand::quoteIdentifier($this->schema) . '.' . DbMysqlOptimizeCommand::quoteIdentifier($this->name);
    }

    /**
     * Returns the query that rebuilds the table online, or fails.
     */
    public function rebuildQuery(): string
    {
        return 'ALTER TABLE ' . $this->quotedName() . ' FORCE, ALGORITHM=INPLACE, LOCK=NONE;';
    }

    /**
     * Whether the table is probably in a shared tablespace, which a rebuild does not shrink.
     */
    public function inSharedTablespace(MysqlServerInfo $server): bool
    {
        return $this->sameFree > 0 || $server->filePerTable === false;
    }

    /**
     * Returns why the table would not be rebuilt, or null.
     */
    public function skipReason(MysqlServerInfo $server): ?string
    {
        if ($this->inSharedTablespace($server)) {
            return 'probably in a shared tablespace: a rebuild would not reclaim space';
        }
        if ($this->offlineIndexes > 0) {
            return 'has a FULLTEXT or SPATIAL index: it cannot be rebuilt without blocking writes';
        }

        return null;
    }
}
