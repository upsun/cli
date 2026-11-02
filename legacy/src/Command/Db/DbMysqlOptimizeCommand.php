<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Db;

use Platformsh\Cli\Command\CommandBase;
use Platformsh\Cli\Console\ArrayArgument;
use Platformsh\Cli\Console\Option;
use Platformsh\Cli\Model\Host\HostInterface;
use Platformsh\Cli\Model\Metrics\MetricKind;
use Platformsh\Cli\Model\Mysql\DiskHeadroom;
use Platformsh\Cli\Model\Mysql\MysqlServerInfo;
use Platformsh\Cli\Model\Mysql\TableCandidate;
use Platformsh\Cli\Selector\Selector;
use Platformsh\Cli\Selector\SelectorConfig;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Io;
use Platformsh\Cli\Service\Observability;
use Platformsh\Cli\Service\QuestionHelper;
use Platformsh\Cli\Service\Relationships;
use Platformsh\Cli\Service\Ssh;
use Platformsh\Cli\Service\Table;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Exception\InvalidArgumentException;
use Symfony\Component\Console\Helper\Helper;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Process\Exception\RuntimeException as ProcessRuntimeException;

/**
 * Finds InnoDB tables whose rebuild would reclaim disk space, and optionally rebuilds them.
 *
 * Replaces "db:size --cleanup".
 */
#[AsCommand(name: 'db:mysql-optimize', description: 'Find MySQL or MariaDB tables with reclaimable disk space, and optionally rebuild them')]
class DbMysqlOptimizeCommand extends CommandBase
{
    protected string $stability = self::STABILITY_BETA;

    private const TABLE_HEADER = [
        'schema' => 'Schema',
        'table' => 'Table',
        'size' => 'Size',
        'reclaimable' => 'Reclaimable',
        'percent' => 'Reclaimable %',
        'disk_needed' => 'Disk needed',
        'disk_fits' => 'Fits disk',
        'tmp_needed' => '/tmp needed',
        'tmp_fits' => 'Fits /tmp',
        'notes' => 'Notes',
    ];
    private const DEFAULT_COLUMNS = ['schema', 'table', 'size', 'reclaimable', 'disk_fits', 'tmp_fits', 'notes'];

    private const DEFAULT_MIN_RECLAIMABLE = '100M';
    private const DEFAULT_MIN_PERCENT = 20;

    /** The percentage of a volume that must stay free after a rebuild. */
    public const RESERVE_PERCENT = 10;

    private const SYSTEM_SCHEMAS = ['mysql', 'information_schema', 'performance_schema', 'sys'];

    /** Rows are sampled in this many ranges of the primary key, of this many rows each. */
    public const SAMPLE_RANGES = 50;
    public const SAMPLE_RANGE_ROWS = 20;

    /** The number of rows sampled from the start of a table, if its primary key is not a single integer column. */
    public const SAMPLE_FIRST_ROWS = 1000;

    /** The bytes per row stored by InnoDB besides the columns (record header, transaction ID and roll pointer). */
    private const ROW_HEADER_BYTES = 20;

    /** The bytes stored for fixed-size column types (at most). Other types are measured with LENGTH(). */
    private const FIXED_SIZES = [
        'tinyint' => 1, 'smallint' => 2, 'mediumint' => 3, 'int' => 4, 'integer' => 4, 'bigint' => 8,
        'float' => 4, 'double' => 8, 'real' => 8, 'bit' => 8,
        'date' => 3, 'time' => 6, 'datetime' => 8, 'timestamp' => 7, 'year' => 1,
        'enum' => 2, 'set' => 8,
    ];
    private const INTEGER_TYPES = ['tinyint', 'smallint', 'mediumint', 'int', 'integer', 'bigint'];

    public function __construct(
        private readonly Config $config,
        private readonly Io $io,
        private readonly Observability $observability,
        private readonly QuestionHelper $questionHelper,
        private readonly Relationships $relationships,
        private readonly Selector $selector,
        private readonly Table $table,
    ) {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this
            ->addOption('min-reclaimable', null, InputOption::VALUE_REQUIRED, 'The minimum reclaimable space of a table, in bytes or with a unit (K, M, G)', self::DEFAULT_MIN_RECLAIMABLE)
            ->addOption('min-percent', null, InputOption::VALUE_REQUIRED, 'The minimum reclaimable space of a table, as a percentage of its size', (string) self::DEFAULT_MIN_PERCENT)
            ->addOption('table', 't', InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY, 'Only check these tables (as "table" or "schema.table")')
            ->addOption('analyze', null, InputOption::VALUE_NONE, 'Refresh statistics with ANALYZE TABLE on the tables found, and check them again (useful on MariaDB and MySQL 5.7)')
            ->addOption('execute', null, InputOption::VALUE_NONE, 'Rebuild the tables found (by default, they are only reported)')
            ->addOption('force', null, InputOption::VALUE_NONE, 'With --execute: rebuild even if the disk headroom is unknown or too small, or on a cluster (Galera)')
            ->addOption('bytes', 'B', InputOption::VALUE_NONE, 'Show sizes in bytes');
        $executable = $this->config->getStr('application.executable');
        $reserve = self::RESERVE_PERCENT;
        $this->setHelp(<<<EOF
            Finds InnoDB tables with free space that a rebuild would return to the disk, e.g. after many rows were deleted.

            The free space is estimated from the free extents reported by the database (DATA_FREE), and from the size
            of the rows: this reads a sample of about 1000 rows from each table larger than <comment>--min-reclaimable</comment>.
            A table is listed if its free space is at least <comment>--min-reclaimable</comment> and <comment>--min-percent</comment> of its size.

            By default, this only reports. With <comment>--execute</comment>, each table listed is rebuilt with:
                <comment>ALTER TABLE ... FORCE, ALGORITHM=INPLACE, LOCK=NONE</comment>
            which is the same rebuild as <comment>OPTIMIZE TABLE</comment>, but fails instead of locking the table if it cannot be done online.

            A rebuild copies the table: it needs free disk space for the rebuilt table, and temporary space for its indexes.
            The disk and /tmp usage are read from the metrics API: if they are unknown, or a table does not fit
            (leaving at least <comment>{$reserve}%</comment> of the volume free), the tables are not rebuilt, unless <comment>--force</comment> is used.

            On a cluster (Galera), a rebuild blocks writes on all nodes while it runs, so it is refused unless <comment>--force</comment> is used.

            The sizes are estimates, from the database's statistics and the sample of rows. To check the disk usage of the service, run: <comment>$executable disk</comment>
            EOF);
        $this->addExample('Report tables that could be optimized');
        $this->addExample('Rebuild the tables found, if there is enough disk space', '--execute');
        $this->addExample('Rebuild one table', '--execute --table main.cache_items');
        $this->selector->addProjectOption($this->getDefinition());
        $this->selector->addEnvironmentOption($this->getDefinition());
        $this->selector->addAppOption($this->getDefinition());
        $this->addCompleter($this->selector);
        Relationships::configureInput($this->getDefinition());
        Table::configureInput($this->getDefinition(), self::TABLE_HEADER, self::DEFAULT_COLUMNS);
        Ssh::configureInput($this->getDefinition());
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $minReclaimable = self::parseSize(Option::string($input, 'min-reclaimable'));
        $minPercent = Option::string($input, 'min-percent');
        if (!is_numeric($minPercent) || (float) $minPercent < 0) {
            throw new InvalidArgumentException('Invalid --min-percent value: ' . $minPercent);
        }
        $minPercent = (float) $minPercent;
        $tableFilter = ArrayArgument::getOption($input, 'table');
        $execute = Option::bool($input, 'execute');
        $force = Option::bool($input, 'force');
        if ($force && !$execute) {
            throw new InvalidArgumentException('The --force option requires --execute.');
        }

        $selection = $this->selector->getSelection($input, new SelectorConfig(allowLocalHost: $this->relationships->hasLocalEnvVar(), chooseEnvFilter: SelectorConfig::filterEnvsMaybeActive()));
        $host = $this->selector->getHostFromSelection($input, $selection);

        $database = $this->chooseMysqlDatabase($host, $input, $output);
        if ($database === null) {
            return 1;
        }
        $mysql = $this->getMysqlCommand($database);
        $serviceName = isset($database['service']) && is_string($database['service']) ? $database['service'] : null;

        $this->stdErr->writeln($serviceName !== null ? \sprintf('Checking database service <comment>%s</comment>...', $serviceName) : 'Checking database...');

        // Read the server's settings.
        $vars = $this->parsePairs((string) $host->runCommand($mysql, true, true, $this->variablesQuery()));
        $server = new MysqlServerInfo($vars);
        $this->io->debug('Server variables: ' . json_encode($vars));

        // Read the disk usage of the service.
        $headroom = null;
        if (!$selection->hasEnvironment() || $serviceName === null) {
            $this->io->debug('No environment or service name: the disk headroom is unknown');
        } elseif (!$this->config->getBool('api.metrics')) {
            $this->io->debug('Metrics are not enabled: the disk headroom is unknown');
        } else {
            try {
                $point = $this->observability->latestServiceMetrics($selection->getEnvironment(), $serviceName, [MetricKind::API_TYPE_DISK], [MetricKind::API_AGG_AVG]);
                $headroom = $point !== null ? DiskHeadroom::fromMetrics($point) : null;
            } catch (\Exception $e) {
                $this->io->debug('Failed to read metrics: ' . $e->getMessage());
            }
        }

        // Find the tables.
        $tables = $this->findTables($host, $mysql, $server, $minReclaimable, $minPercent, $tableFilter);
        if ($tables && Option::bool($input, 'analyze')) {
            $this->stdErr->writeln(\sprintf('Analyzing %d table(s)...', \count($tables)));
            $host->runCommand($mysql, true, true, 'ANALYZE TABLE ' . implode(', ', array_map(fn(TableCandidate $t): string => $t->quotedName(), $tables)) . ';');
            $tables = $this->findTables($host, $mysql, $server, $minReclaimable, $minPercent, $tableFilter);
        }

        $this->stdErr->writeln('');
        $this->showServerWarnings($server, $headroom);

        if (!$tables) {
            $this->stdErr->writeln(\sprintf(
                'No tables were found with at least <info>%s</info> and <info>%s%%</info> of their size reclaimable.',
                Helper::formatMemory($minReclaimable),
                $minPercent,
            ));
            return 0;
        }

        $this->renderTables($tables, $headroom, $server, Option::bool($input, 'bytes'));

        if (!$execute) {
            if (!$this->table->formatIsMachineReadable()) {
                $this->stdErr->writeln('');
                $this->stdErr->writeln('The reclaimable space is an estimate. Rebuilding a table copies it, and may take a long time.');
                $this->stdErr->writeln(\sprintf('To rebuild these tables, run this command again with <comment>--execute</comment>%s.', $tableFilter ? '' : ' (and <comment>--table</comment> to choose tables)'));
            }
            return 0;
        }

        return $this->rebuild($host, $mysql, $tables, $server, $headroom, $force) ? 0 : 1;
    }

    /**
     * Chooses a MySQL or MariaDB relationship.
     *
     * @return array<string, mixed>|null
     */
    private function chooseMysqlDatabase(HostInterface $host, InputInterface $input, OutputInterface $output): ?array
    {
        // Check an explicit relationship's scheme, for a clear error.
        if (Option::stringOrNull($input, 'relationship') !== null) {
            $database = $this->relationships->chooseService($host, $input, $output);
            if (!$database) {
                return null;
            }
            if ($database['scheme'] !== 'mysql') {
                $this->stdErr->writeln(\sprintf(
                    'The relationship <error>%s</error> is not a MySQL or MariaDB database (its scheme is: %s).',
                    $database['_relationship_name'],
                    $database['scheme'],
                ));
                $this->stdErr->writeln('This command only supports MySQL and MariaDB.');
                return null;
            }
            return $database;
        }
        $database = $this->relationships->chooseDatabase($host, $input, $output, ['mysql']);
        if (!$database) {
            $this->stdErr->writeln('This command only supports MySQL and MariaDB.');
            return null;
        }

        return $database;
    }

    /**
     * @param array<string, mixed> $database
     */
    private function getMysqlCommand(array $database): string
    {
        $cmdName = $this->relationships->isMariaDB($database) ? 'mariadb' : 'mysql';

        return \sprintf(
            '%s %s --no-auto-rehash --batch --raw --skip-column-names',
            $this->relationships->mariaDbCommandWithFallback($cmdName),
            $this->relationships->getDbCommandArgs($cmdName, $database, ''),
        );
    }

    private function variablesQuery(): string
    {
        $vars = ['version', 'version_comment', 'innodb_file_per_table', 'wsrep_on', 'tmpdir', 'innodb_tmpdir', 'information_schema_stats_expiry'];

        return \sprintf(
            "SHOW GLOBAL VARIABLES WHERE Variable_name IN (%s);\nSHOW GLOBAL STATUS WHERE Variable_name IN ('wsrep_cluster_size');\n",
            implode(', ', array_map(fn(string $v): string => "'" . $v . "'", $vars)),
        );
    }

    /**
     * Parses tab-separated name/value lines.
     *
     * @return array<string, string>
     */
    private function parsePairs(string $output): array
    {
        $pairs = [];
        foreach (explode("\n", $output) as $line) {
            $parts = explode("\t", rtrim($line, "\r"), 2);
            if (\count($parts) === 2) {
                $pairs[strtolower($parts[0])] = $parts[1];
            }
        }

        return $pairs;
    }

    /**
     * Finds InnoDB tables with reclaimable space.
     *
     * @param string[] $filter
     *
     * @return TableCandidate[]
     */
    private function findTables(HostInterface $host, string $mysql, MysqlServerInfo $server, int $minReclaimable, float $minPercent, array $filter): array
    {
        $query = '';
        if ($server->hasStatsExpiry) {
            // MySQL 8 caches statistics in information_schema for up to 24 hours by default.
            $query .= "SET SESSION information_schema_stats_expiry = 0;\n";
        }
        $query .= self::tablesQuery($minReclaimable, $filter) . "\n";
        $this->io->debug('Query: ' . $query);
        $tables = [];
        foreach ($this->parseRows((string) $host->runCommand($mysql, true, true, $query), 8) as $fields) {
            $tables[] = TableCandidate::fromRow($fields);
        }

        $tables = $this->sampleRows($host, $mysql, $server, $tables);

        $tables = array_values(array_filter($tables, fn(TableCandidate $t): bool => $t->reclaimable() >= $minReclaimable
            && $t->reclaimable() * 100 >= $minPercent * $t->size()));
        usort($tables, fn(TableCandidate $a, TableCandidate $b): int => $b->reclaimable() <=> $a->reclaimable());

        return $tables;
    }

    /**
     * Estimates the size of the rows of each table, from a sample.
     *
     * Tables in a shared tablespace are not sampled, as a rebuild would not shrink it.
     * If the sample cannot be read, the tables are returned unchanged.
     *
     * @param TableCandidate[] $tables
     *
     * @return TableCandidate[]
     */
    private function sampleRows(HostInterface $host, string $mysql, MysqlServerInfo $server, array $tables): array
    {
        $toSample = array_filter($tables, fn(TableCandidate $t): bool => !$t->inSharedTablespace($server));
        if (!$toSample) {
            return $tables;
        }

        try {
            $query = self::columnsQuery($toSample);
            $this->io->debug('Query: ' . $query);
            $columns = [];
            foreach ($this->parseRows((string) $host->runCommand($mysql, true, true, $query), 5) as $fields) {
                $columns[$fields[0] . "\0" . $fields[1]][] = ['name' => $fields[2], 'type' => strtolower($fields[3]), 'primary' => $fields[4] === 'PRI'];
            }

            $query = '';
            $firstRows = [];
            foreach ($toSample as $i => $t) {
                $tableColumns = $columns[$t->schema . "\0" . $t->name] ?? [];
                if ($tableColumns) {
                    $query .= self::sampleQuery($i, $t, $tableColumns) . "\n";
                    $firstRows[$i] = self::integerPrimaryKey($tableColumns) === null;
                }
            }
            if ($query === '') {
                return $tables;
            }
            $this->stdErr->writeln(\sprintf('Sampling rows from %d table(s)...', \count($firstRows)));
            $this->io->debug('Query: ' . $query);
            $result = (string) $host->runCommand($mysql, true, true, $query);
        } catch (ProcessRuntimeException $e) {
            $this->stdErr->writeln('<comment>Warning:</comment> failed to sample rows: only free extents are counted as reclaimable.');
            $this->io->debug($e->getMessage());
            return $tables;
        }

        foreach ($this->parseRows($result, 3) as [$i, $count, $bytes]) {
            if (isset($firstRows[(int) $i])) {
                $tables[(int) $i] = $tables[(int) $i]->withSample((int) $count > 0 ? (int) $bytes / (int) $count : 0.0, $firstRows[(int) $i]);
            }
        }

        return $tables;
    }

    /**
     * Parses tab-separated lines, with the given number of fields.
     *
     * @return list<string[]>
     */
    private function parseRows(string $output, int $numFields): array
    {
        $rows = [];
        foreach (explode("\n", $output) as $line) {
            $fields = explode("\t", rtrim($line, "\r"));
            if (\count($fields) === $numFields) {
                $rows[] = $fields;
            }
        }

        return $rows;
    }

    /**
     * Returns the query to find InnoDB tables that may have reclaimable space.
     *
     * The thresholds are applied after rows are sampled: here, a table is
     * only excluded if its total size is below the minimum.
     *
     * Free space in a shared tablespace (the system tablespace, if
     * innodb_file_per_table is OFF, or a general tablespace) is reported for
     * each of its tables: the "same_free" column counts the other tables with
     * the same free space, to detect them.
     *
     * @param string[] $filter
     */
    public static function tablesQuery(int $minReclaimable, array $filter = []): string
    {
        $conditions = [
            "t.ENGINE = 'InnoDB'",
            "t.TABLE_TYPE = 'BASE TABLE'",
            't.TABLE_SCHEMA NOT IN (' . implode(', ', array_map(self::quoteString(...), self::SYSTEM_SCHEMAS)) . ')',
            't.DATA_LENGTH + t.INDEX_LENGTH + t.DATA_FREE >= ' . $minReclaimable,
        ];
        if ($filter) {
            $or = [];
            foreach ($filter as $name) {
                if (str_contains($name, '.')) {
                    [$schema, $table] = explode('.', $name, 2);
                    $or[] = \sprintf('(t.TABLE_SCHEMA = %s AND t.TABLE_NAME = %s)', self::quoteString($schema), self::quoteString($table));
                } else {
                    $or[] = \sprintf('t.TABLE_NAME = %s', self::quoteString($name));
                }
            }
            $conditions[] = '(' . implode(' OR ', $or) . ')';
        }

        return 'SELECT t.TABLE_SCHEMA, t.TABLE_NAME, t.DATA_LENGTH, t.INDEX_LENGTH, t.DATA_FREE, t.TABLE_ROWS,'
            . " (SELECT COUNT(*) FROM information_schema.STATISTICS s WHERE s.TABLE_SCHEMA = t.TABLE_SCHEMA AND s.TABLE_NAME = t.TABLE_NAME AND s.INDEX_TYPE IN ('FULLTEXT', 'SPATIAL')) AS offline_indexes,"
            . ' COALESCE(f.n, 1) - 1 AS same_free'
            . ' FROM information_schema.TABLES t'
            . " LEFT JOIN (SELECT DATA_FREE AS free, COUNT(*) AS n FROM information_schema.TABLES WHERE ENGINE = 'InnoDB' AND DATA_FREE >= " . $minReclaimable . ' GROUP BY DATA_FREE) f ON f.free = t.DATA_FREE'
            . ' WHERE ' . implode(' AND ', $conditions) . ';';
    }

    /**
     * Returns the query to find the stored columns of tables.
     *
     * @param TableCandidate[] $tables
     */
    public static function columnsQuery(array $tables): string
    {
        $names = array_map(fn(TableCandidate $t): string => \sprintf('(%s, %s)', self::quoteString($t->schema), self::quoteString($t->name)), $tables);

        return 'SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE, COLUMN_KEY FROM information_schema.COLUMNS'
            . ' WHERE (TABLE_SCHEMA, TABLE_NAME) IN (' . implode(', ', $names) . ")"
            . " AND EXTRA NOT LIKE '%VIRTUAL%'"
            . ' ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION;';
    }

    /**
     * Returns the query to sample the rows of a table: it outputs the index, the number of rows sampled, and their bytes.
     *
     * With a single-column integer primary key, rows are read from ranges spread across the key's values: each range
     * is an index lookup. The ranges start at uneven intervals (a golden ratio sequence), so that they do not all
     * fall at the same point of a pattern that repeats in the data. Otherwise, the first rows are read.
     *
     * @param array{name: string, type: string, primary: bool}[] $columns
     */
    public static function sampleQuery(int $index, TableCandidate $table, array $columns): string
    {
        $expr = self::rowBytesExpression($columns);
        $from = $table->quotedName();
        $pk = self::integerPrimaryKey($columns);
        if ($pk === null) {
            return \sprintf('SELECT %d, COUNT(*), COALESCE(SUM(b), 0) FROM (SELECT %s AS b FROM %s LIMIT %d) s;', $index, $expr, $from, self::SAMPLE_FIRST_ROWS);
        }
        $pk = self::quoteIdentifier($pk);
        $ranges = [];
        for ($i = 0; $i < self::SAMPLE_RANGES; $i++) {
            $ranges[] = \sprintf(
                '(SELECT %s AS b FROM %s WHERE %s >= @lo + FLOOR((CAST(@hi AS DECIMAL(65)) - @lo) * %.6F) ORDER BY %s LIMIT %d)',
                $expr,
                $from,
                $pk,
                fmod($i * 0.6180339887, 1),
                $pk,
                self::SAMPLE_RANGE_ROWS,
            );
        }

        return \sprintf('SELECT MIN(%s), MAX(%s) FROM %s INTO @lo, @hi;', $pk, $pk, $from)
            . \sprintf(' SELECT %d, COUNT(*), COALESCE(SUM(b), 0) FROM (%s) s;', $index, implode(' UNION ALL ', $ranges));
    }

    /**
     * Returns an expression for the approximate bytes that InnoDB stores for a row.
     *
     * @param array{name: string, type: string, primary: bool}[] $columns
     */
    public static function rowBytesExpression(array $columns): string
    {
        $fixed = self::ROW_HEADER_BYTES;
        $variable = [];
        foreach ($columns as $column) {
            if (isset(self::FIXED_SIZES[$column['type']])) {
                $fixed += self::FIXED_SIZES[$column['type']];
            } else {
                // Plus a byte for the length.
                $fixed++;
                $variable[] = \sprintf('COALESCE(LENGTH(%s), 0)', self::quoteIdentifier($column['name']));
            }
        }

        return implode(' + ', array_merge([(string) $fixed], $variable));
    }

    /**
     * Returns the name of the primary key column, if the key is a single integer column.
     *
     * @param array{name: string, type: string, primary: bool}[] $columns
     */
    private static function integerPrimaryKey(array $columns): ?string
    {
        $primary = array_values(array_filter($columns, fn(array $c): bool => $c['primary']));

        return \count($primary) === 1 && \in_array($primary[0]['type'], self::INTEGER_TYPES, true) ? $primary[0]['name'] : null;
    }

    /**
     * Shows warnings about the server's configuration.
     */
    private function showServerWarnings(MysqlServerInfo $server, ?DiskHeadroom $headroom): void
    {
        if ($server->filePerTable === false) {
            $this->stdErr->writeln('<comment>Warning:</comment> innodb_file_per_table is OFF: tables are stored in the shared system tablespace, which does not shrink when they are rebuilt.');
        }
        if ($server->isGaleraCluster()) {
            $this->stdErr->writeln(\sprintf('<comment>Warning:</comment> the database is a Galera cluster (%d nodes): rebuilding a table blocks writes on all nodes while it runs.', $server->clusterSize));
        }
        if ($headroom === null) {
            $this->stdErr->writeln('<comment>Warning:</comment> the disk usage of the service is unknown (metrics are not available), so it is not known whether a rebuild would fit.');
        } else {
            $this->stdErr->writeln(\sprintf(
                'Disk: <info>%s</info> free of %s. /tmp: %s.',
                Helper::formatMemory((int) $headroom->diskFree()),
                Helper::formatMemory((int) $headroom->diskLimit),
                $headroom->hasTmp() ? '<info>' . Helper::formatMemory((int) $headroom->tmpFree()) . '</info> free of ' . Helper::formatMemory((int) $headroom->tmpLimit) : 'unknown',
            ));
            if ($server->tmpdir() !== null && !str_starts_with($server->tmpdir(), '/tmp')) {
                $this->stdErr->writeln(\sprintf('Note: the server\'s temporary directory is %s, not /tmp.', $server->tmpdir()));
            }
        }
        $this->stdErr->writeln('');
    }

    /**
     * @param TableCandidate[] $tables
     */
    private function renderTables(array $tables, ?DiskHeadroom $headroom, MysqlServerInfo $server, bool $bytes): void
    {
        $machineReadable = $this->table->formatIsMachineReadable();
        $formatSize = fn(int $size): string => $bytes || $machineReadable ? (string) $size : Helper::formatMemory($size);
        $formatFits = function (?bool $fits) use ($machineReadable): string {
            if ($fits === null) {
                return $machineReadable ? '' : 'unknown';
            }
            if ($machineReadable) {
                return $fits ? 'true' : 'false';
            }
            return $fits ? '<info>yes</info>' : '<error>no</error>';
        };
        $rows = [];
        foreach ($tables as $t) {
            $rows[] = [
                'schema' => $t->schema,
                'table' => $t->name,
                'size' => $formatSize($t->size()),
                'reclaimable' => $formatSize($t->reclaimable()),
                'percent' => $t->size() > 0 ? \sprintf('%.0f%%', $t->reclaimable() / $t->size() * 100) : '',
                'disk_needed' => $formatSize($t->diskNeeded()),
                'disk_fits' => $formatFits($headroom?->diskFits($t->diskNeeded())),
                'tmp_needed' => $formatSize($t->tmpNeeded()),
                'tmp_fits' => $formatFits($headroom?->tmpFits($t->tmpNeeded())),
                'notes' => implode('; ', array_filter([
                    $t->skipReason($server),
                    $t->sampledFirstRows ? 'estimated from the first rows only' : null,
                ])),
            ];
        }
        $this->table->render($rows, self::TABLE_HEADER, self::DEFAULT_COLUMNS);
    }

    /**
     * Rebuilds tables, after checking that it is safe.
     *
     * @param TableCandidate[] $tables
     *
     * @return bool Whether the rebuilds succeeded, or there were none to run.
     */
    private function rebuild(HostInterface $host, string $mysql, array $tables, MysqlServerInfo $server, ?DiskHeadroom $headroom, bool $force): bool
    {
        $this->stdErr->writeln('');

        $toRebuild = [];
        foreach ($tables as $t) {
            if ($reason = $t->skipReason($server)) {
                $this->stdErr->writeln(\sprintf('Skipping <comment>%s</comment>: %s', $t->displayName(), $reason));
                continue;
            }
            $toRebuild[] = $t;
        }
        if (!$toRebuild) {
            $this->stdErr->writeln('No tables can be rebuilt.');
            return false;
        }

        $problems = [];
        if ($headroom === null) {
            $problems[] = 'The disk usage of the service is unknown.';
        } else {
            foreach ($toRebuild as $t) {
                if (!$headroom->diskFits($t->diskNeeded())) {
                    $problems[] = \sprintf('There is not enough free disk space to rebuild %s (it needs about %s).', $t->displayName(), Helper::formatMemory($t->diskNeeded()));
                }
                if ($headroom->tmpFits($t->tmpNeeded()) === false) {
                    $problems[] = \sprintf('There is not enough free space in /tmp to rebuild %s (it may need up to %s).', $t->displayName(), Helper::formatMemory($t->tmpNeeded()));
                }
            }
        }
        if ($server->isGaleraCluster()) {
            $problems[] = 'The database is a Galera cluster: a rebuild blocks writes on all nodes while it runs.';
        }
        if ($problems) {
            foreach ($problems as $problem) {
                $this->stdErr->writeln(($force ? '<comment>Warning:</comment> ' : '<error>Error:</error> ') . $problem);
            }
            if (!$force) {
                $this->stdErr->writeln('');
                $this->stdErr->writeln('The tables were not rebuilt. Use <comment>--force</comment> to rebuild them anyway, at your own risk: a rebuild that fills the disk can stop the database.');
                return false;
            }
            $this->stdErr->writeln('');
        }

        $this->stdErr->writeln(\sprintf('Rebuilding %d table(s), one at a time. Each rebuild copies the table, and may take a long time.', \count($toRebuild)));
        if (!$this->questionHelper->confirm('Are you sure you want to continue?')) {
            return false;
        }

        foreach ($toRebuild as $t) {
            $sql = $t->rebuildQuery();
            $this->stdErr->write(\sprintf('Rebuilding <info>%s</info>...', $t->displayName()));
            $this->io->debug('Query: ' . $sql);
            $start = microtime(true);
            try {
                $host->runCommand($mysql, true, true, $sql);
            } catch (ProcessRuntimeException $e) {
                $this->stdErr->writeln(' <error>[failed]</error>');
                $this->stdErr->writeln($e->getMessage());
                return false;
            }
            $this->stdErr->writeln(\sprintf(' <info>[OK]</info> (%ds)', round(microtime(true) - $start)));
        }

        $this->stdErr->writeln('');
        $this->stdErr->writeln('Run this command again (without <comment>--execute</comment>) to check the result.');

        return true;
    }

    /**
     * Parses a size, in bytes or with a unit (K, M, G or T, in powers of 1024).
     */
    public static function parseSize(string $value): int
    {
        if (!preg_match('/^\s*(\d+(?:\.\d+)?)\s*([kmgt])?i?b?\s*$/i', $value, $matches)) {
            throw new InvalidArgumentException('Invalid size: ' . $value);
        }
        $exponent = isset($matches[2]) ? (int) strpos('kmgt', strtolower($matches[2])) + 1 : 0;

        return (int) round((float) $matches[1] * (1024 ** $exponent));
    }

    /**
     * Quotes a MySQL identifier.
     */
    public static function quoteIdentifier(string $name): string
    {
        return '`' . str_replace('`', '``', $name) . '`';
    }

    /**
     * Quotes a MySQL string literal.
     */
    public static function quoteString(string $value): string
    {
        return "'" . str_replace(['\\', "'"], ['\\\\', "''"], $value) . "'";
    }
}
