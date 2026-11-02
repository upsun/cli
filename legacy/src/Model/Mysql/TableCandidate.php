<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model\Mysql;

use Platformsh\Cli\Command\Db\DbMysqlOptimizeCommand;

/**
 * An InnoDB table with reclaimable space, from information_schema.
 */
readonly class TableCandidate
{
    public function __construct(
        public string $schema,
        public string $name,
        public int $dataLength,
        public int $indexLength,
        public int $dataFree,
        /** The number of FULLTEXT or SPATIAL indexes, which prevent an online rebuild. */
        public int $offlineIndexes = 0,
        /** The number of other tables reporting the same free space (a sign of a shared tablespace). */
        public int $sameFree = 0,
    ) {}

    /**
     * @param string[] $row
     */
    public static function fromRow(array $row): self
    {
        return new self($row[0], $row[1], (int) $row[2], (int) $row[3], (int) $row[4], (int) $row[5], (int) $row[6]);
    }

    /**
     * The size of the table's data and indexes, excluding free space.
     */
    public function size(): int
    {
        return $this->dataLength + $this->indexLength;
    }

    /**
     * The disk space needed for the rebuilt copy of the table.
     */
    public function diskNeeded(): int
    {
        return $this->size();
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
     * Returns why the table would not be rebuilt, or null.
     */
    public function skipReason(MysqlServerInfo $server): ?string
    {
        if ($this->sameFree > 0 || $server->filePerTable === false) {
            return 'probably in a shared tablespace: a rebuild would not reclaim space';
        }
        if ($this->offlineIndexes > 0) {
            return 'has a FULLTEXT or SPATIAL index: it cannot be rebuilt without blocking writes';
        }

        return null;
    }
}
