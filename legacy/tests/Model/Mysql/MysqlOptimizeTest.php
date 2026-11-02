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

    public function testSkipReason(): void
    {
        $server = new MysqlServerInfo(['innodb_file_per_table' => 'ON']);
        $this->assertNull((new TableCandidate('s', 't', 1, 1, 1))->skipReason($server));
        $this->assertStringContainsString('shared tablespace', (string) (new TableCandidate('s', 't', 1, 1, 1, 0, 2))->skipReason($server));
        $this->assertStringContainsString('FULLTEXT', (string) (new TableCandidate('s', 't', 1, 1, 1, 1))->skipReason($server));
        $this->assertStringContainsString('shared tablespace', (string) (new TableCandidate('s', 't', 1, 1, 1))->skipReason(new MysqlServerInfo(['innodb_file_per_table' => 'OFF'])));
    }
}
