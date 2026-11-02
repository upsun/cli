<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Model\Mysql;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Command\Db\DbMysqlOptimizeCommand;
use Platformsh\Cli\Model\Mysql\DiskHeadroom;
use Platformsh\Cli\Model\Mysql\MysqlServerInfo;
use Platformsh\Cli\Model\Mysql\TableCandidate;
use Symfony\Component\Console\Exception\InvalidArgumentException;

class MysqlOptimizeTest extends TestCase
{
    public function testParseSize(): void
    {
        $cases = [
            ['100', 100],
            ['100M', 100 * 1024 * 1024],
            ['1.5G', (int) (1.5 * 1024 ** 3)],
            ['2 GiB', 2 * 1024 ** 3],
            ['10kb', 10 * 1024],
        ];
        foreach ($cases as [$input, $expected]) {
            $this->assertSame($expected, DbMysqlOptimizeCommand::parseSize($input), $input);
        }
        $this->expectException(InvalidArgumentException::class);
        DbMysqlOptimizeCommand::parseSize('-1M');
    }

    public function testQuoting(): void
    {
        $this->assertSame('`a``b`', DbMysqlOptimizeCommand::quoteIdentifier('a`b'));
        $this->assertSame("'a''b\\\\c'", DbMysqlOptimizeCommand::quoteString("a'b\\c"));
        $table = new TableCandidate('my`db', 't', 1, 2, 3);
        $this->assertSame('ALTER TABLE `my``db`.`t` FORCE, ALGORITHM=INPLACE, LOCK=NONE;', $table->rebuildQuery());
    }

    public function testServerInfo(): void
    {
        $server = new MysqlServerInfo(['version' => '10.11.8-MariaDB', 'wsrep_on' => 'ON', 'wsrep_cluster_size' => '3', 'innodb_tmpdir' => '', 'tmpdir' => '/tmp']);
        $this->assertTrue($server->isMariaDB);
        $this->assertTrue($server->isGaleraCluster());
        $this->assertNull($server->filePerTable);
        $this->assertFalse($server->hasStatsExpiry);
        $this->assertSame('/tmp', $server->tmpdir());

        // A single-node cluster does not block other nodes.
        $server = new MysqlServerInfo(['version' => '8.0.36', 'wsrep_on' => 'ON', 'wsrep_cluster_size' => '1', 'innodb_file_per_table' => 'OFF', 'information_schema_stats_expiry' => '86400']);
        $this->assertFalse($server->isMariaDB);
        $this->assertFalse($server->isGaleraCluster());
        $this->assertFalse($server->filePerTable);
        $this->assertTrue($server->hasStatsExpiry);
    }

    public function testHeadroom(): void
    {
        $this->assertNull(DiskHeadroom::fromMetrics([]));
        $headroom = DiskHeadroom::fromMetrics(['mountpoints' => [
            '/mnt' => ['disk_used' => ['avg' => 500], 'disk_limit' => ['max' => 1000]],
        ]]);
        $this->assertNotNull($headroom);
        $this->assertFalse($headroom->hasTmp());
        $this->assertNull($headroom->tmpFits(1));
        // At least 10% of the volume stays free.
        $this->assertTrue($headroom->diskFits(400));
        $this->assertFalse($headroom->diskFits(401));
    }

    public function testEstimates(): void
    {
        $mib = 1024 * 1024;
        // 90 MiB of data and 10 MiB of indexes, with 5 MiB of free extents.
        $table = new TableCandidate('s', 't', 90 * $mib, 10 * $mib, 5 * $mib, rows: 10240);

        // The size includes the free extents.
        $this->assertSame(105 * $mib, $table->size());

        // Without a sample, only the free extents are reclaimable, and a rebuild needs the current data and indexes.
        $this->assertNull($table->rebuiltSize());
        $this->assertSame(5 * $mib, $table->reclaimable());
        $this->assertSame(100 * $mib, $table->diskNeeded());

        // 10240 rows of 1 KiB: 15 MiB of data, and the indexes shrink in the same ratio (to 1/6).
        $sampled = $table->withSample(1024, false);
        $rebuilt = 15 * $mib + (int) round(10 * $mib / 6);
        $this->assertSame($rebuilt, $sampled->rebuiltSize());
        $this->assertSame(105 * $mib - $rebuilt, $sampled->reclaimable());
        $this->assertSame($rebuilt + intdiv($rebuilt, 10) + 8 * $mib, $sampled->diskNeeded());

        // A table that would grow: the indexes are not scaled up, and the free extents are still reclaimable.
        $growing = $table->withSample(10240, false);
        $this->assertSame(160 * $mib, $growing->rebuiltSize());
        $this->assertSame(5 * $mib, $growing->reclaimable());

        // An empty table.
        $this->assertSame(105 * $mib, (new TableCandidate('s', 't', 90 * $mib, 10 * $mib, 5 * $mib))->withSample(0, false)->reclaimable());
    }

    public function testSampleQuery(): void
    {
        $columns = [
            ['name' => 'id', 'type' => 'bigint', 'primary' => true],
            ['name' => 'na`me', 'type' => 'varchar', 'primary' => false],
            ['name' => 'created', 'type' => 'datetime', 'primary' => false],
            ['name' => 'body', 'type' => 'text', 'primary' => false],
        ];
        // 20 bytes of header, 8 + 8 for the fixed columns, and a byte for each variable length.
        $this->assertSame('38 + COALESCE(LENGTH(`na``me`), 0) + COALESCE(LENGTH(`body`), 0)', DbMysqlOptimizeCommand::rowBytesExpression($columns));

        $table = new TableCandidate('my`db', 't', 1, 2, 3);
        $query = DbMysqlOptimizeCommand::sampleQuery(3, $table, $columns);
        $this->assertStringStartsWith('SELECT MIN(`id`), MAX(`id`) FROM `my``db`.`t` INTO @lo, @hi; SELECT 3, COUNT(*), COALESCE(SUM(b), 0) FROM ((SELECT 38 + ', $query);
        $this->assertSame(DbMysqlOptimizeCommand::SAMPLE_RANGES, substr_count($query, 'ORDER BY `id` LIMIT 20)'));
        $this->assertStringContainsString('WHERE `id` >= @lo + FLOOR((CAST(@hi AS DECIMAL(65)) - @lo) * 0.000000) ORDER BY', $query);
        $this->assertStringContainsString('* 0.618034) ORDER BY', $query);
        $this->assertStringContainsString('* 0.236068) ORDER BY', $query);

        // Without a single-column integer primary key, the first rows are read.
        $expected = 'SELECT 0, COUNT(*), COALESCE(SUM(b), 0) FROM (SELECT 23 + COALESCE(LENGTH(`code`), 0) AS b FROM `my``db`.`t` LIMIT 1000) s;';
        $this->assertSame($expected, DbMysqlOptimizeCommand::sampleQuery(0, $table, [
            ['name' => 'code', 'type' => 'varchar', 'primary' => true],
            ['name' => 'n', 'type' => 'smallint', 'primary' => false],
        ]));
        $this->assertStringEndsWith('LIMIT 1000) s;', DbMysqlOptimizeCommand::sampleQuery(0, $table, [
            ['name' => 'a', 'type' => 'int', 'primary' => true],
            ['name' => 'b', 'type' => 'int', 'primary' => true],
        ]));
    }

    public function testSkipReason(): void
    {
        $server = new MysqlServerInfo(['innodb_file_per_table' => 'ON']);
        $this->assertNull((new TableCandidate('s', 't', 1, 1, 1))->skipReason($server));
        $this->assertStringContainsString('shared tablespace', (string) (new TableCandidate('s', 't', 1, 1, 1, sameFree: 2))->skipReason($server));
        $this->assertStringContainsString('FULLTEXT', (string) (new TableCandidate('s', 't', 1, 1, 1, offlineIndexes: 1))->skipReason($server));
        $this->assertStringContainsString('shared tablespace', (string) (new TableCandidate('s', 't', 1, 1, 1))->skipReason(new MysqlServerInfo(['innodb_file_per_table' => 'OFF'])));
    }
}
