<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model\Mysql;

/**
 * Settings of a MySQL or MariaDB server, read from its global variables and status.
 */
readonly class MysqlServerInfo
{
    public string $version;
    public bool $isMariaDB;
    /** Whether innodb_file_per_table is ON, or null if it is unknown. */
    public ?bool $filePerTable;
    public bool $wsrepOn;
    public int $clusterSize;
    /** Whether information_schema_stats_expiry exists (MySQL 8+). */
    public bool $hasStatsExpiry;
    private ?string $tmpdir;

    /**
     * @param array<string, string> $vars Variables and status values, by lowercase name.
     */
    public function __construct(array $vars)
    {
        $this->version = $vars['version'] ?? '';
        $this->isMariaDB = stripos($this->version . ' ' . ($vars['version_comment'] ?? ''), 'mariadb') !== false;
        $this->filePerTable = isset($vars['innodb_file_per_table']) ? self::bool($vars['innodb_file_per_table']) : null;
        $this->wsrepOn = isset($vars['wsrep_on']) && self::bool($vars['wsrep_on']);
        $this->clusterSize = (int) ($vars['wsrep_cluster_size'] ?? 0);
        $this->hasStatsExpiry = isset($vars['information_schema_stats_expiry']);
        $tmpdir = ($vars['innodb_tmpdir'] ?? '') !== '' ? $vars['innodb_tmpdir'] : ($vars['tmpdir'] ?? '');
        $this->tmpdir = $tmpdir !== '' ? $tmpdir : null;
    }

    /**
     * Returns whether the server is a node of a Galera cluster with more than one node.
     */
    public function isGaleraCluster(): bool
    {
        return $this->wsrepOn && $this->clusterSize > 1;
    }

    /**
     * Returns the directory for temporary files of online DDL (innodb_tmpdir, or tmpdir).
     */
    public function tmpdir(): ?string
    {
        return $this->tmpdir;
    }

    private static function bool(string $value): bool
    {
        return \in_array(strtoupper($value), ['ON', '1', 'TRUE', 'YES'], true);
    }
}
