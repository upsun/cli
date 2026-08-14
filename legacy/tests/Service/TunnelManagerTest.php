<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Selector\Selection;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Io;
use Platformsh\Cli\Service\Relationships;
use Platformsh\Cli\Service\TunnelManager;
use Platformsh\Cli\Tests\HasTempDirTrait;
use Platformsh\Cli\Tunnel\Tunnel;
use Platformsh\Client\Model\Environment;
use Platformsh\Client\Model\Project;

class TunnelManagerTest extends TestCase
{
    use HasTempDirTrait;

    public function setUp(): void
    {
        $this->tempDirSetUp();
    }

    private function createManager(?string $writableUserDir = null): TunnelManager
    {
        $config = $this->createMock(Config::class);
        if ($writableUserDir !== null) {
            $config->method('getWritableUserDir')->willReturn($writableUserDir);
        }
        $io = $this->createMock(Io::class);
        $relationships = $this->createMock(Relationships::class);

        return new TunnelManager($config, $io, $relationships);
    }

    /**
     * @param array<string, mixed> $entry
     */
    private function writeTunnelInfo(string $id, array $entry): string
    {
        assert($this->tempDir !== null);
        $filename = $this->tempDir . '/tunnel-info.json';
        if (file_put_contents($filename, (string) json_encode([$id => $entry + ['id' => $id]])) === false) {
            throw new \RuntimeException('Failed to write: ' . $filename);
        }

        return $filename;
    }

    /**
     * Calls the private unserialize() method via reflection.
     *
     * @return Tunnel[]
     */
    private function callUnserialize(TunnelManager $manager, string $json): array
    {
        $method = new \ReflectionMethod($manager, 'unserialize');

        return $method->invoke($manager, $json);
    }

    public function testUnserializeNewFormat(): void
    {
        $tunnels = $this->callUnserialize($this->createManager(), (string) json_encode([
            'proj1--main--app--database--0' => [
                'projectId' => 'proj1',
                'environmentId' => 'main',
                'appName' => 'app',
                'relationship' => 'database',
                'serviceKey' => 0,
                'service' => ['scheme' => 'mysql', 'host' => 'database.internal', 'port' => 3306],
                'id' => 'proj1--main--app--database--0',
                'localPort' => 30000,
                'remoteHost' => 'database.internal',
                'remotePort' => 3306,
                'pid' => 12345,
            ],
        ]));

        $this->assertCount(1, $tunnels);
        $tunnel = $tunnels[0];
        $this->assertSame('proj1--main--app--database--0', $tunnel->id);
        $this->assertSame(30000, $tunnel->localPort);
        $this->assertSame('database.internal', $tunnel->remoteHost);
        $this->assertSame(3306, $tunnel->remotePort);
        $this->assertSame(12345, $tunnel->pid);
        $this->assertSame('proj1', $tunnel->metadata['projectId']);
    }

    public function testUnserializeOldFormatWithoutId(): void
    {
        // 4.x-style tunnel-info.json: no 'id' field in the entry.
        $tunnels = $this->callUnserialize($this->createManager(), (string) json_encode([
            'some-old-key' => [
                'projectId' => 'abc123',
                'environmentId' => 'staging',
                'appName' => 'web',
                'relationship' => 'redis',
                'serviceKey' => 1,
                'service' => ['scheme' => 'redis', 'host' => 'redis.internal', 'port' => 6379],
                'localPort' => 30001,
                'remoteHost' => 'redis.internal',
                'remotePort' => 6379,
                'pid' => 99999,
            ],
        ]));

        $this->assertCount(1, $tunnels);
        $tunnel = $tunnels[0];
        // The ID should be derived from metadata fields.
        $this->assertSame('abc123--staging--web--redis--1', $tunnel->id);
        $this->assertSame(30001, $tunnel->localPort);
        $this->assertSame('redis.internal', $tunnel->remoteHost);
        $this->assertSame(6379, $tunnel->remotePort);
        $this->assertSame(99999, $tunnel->pid);
        $this->assertSame('abc123', $tunnel->metadata['projectId']);
        $this->assertSame('staging', $tunnel->metadata['environmentId']);
        $this->assertSame('web', $tunnel->metadata['appName']);
        $this->assertSame('redis', $tunnel->metadata['relationship']);
        $this->assertSame(1, $tunnel->metadata['serviceKey']);
    }

    public function testCreateCastsRemotePortToInt(): void
    {
        // Relationship data from the API delivers 'port' as a string, but
        // Tunnel::$remotePort is typed int. Regression test for #72.
        $selection = new Selection(
            null,
            new Project(['id' => 'proj1']),
            new Environment(['id' => 'main']),
            'app',
        );
        $service = [
            'host' => 'database.internal',
            'port' => '3306',
            '_relationship_name' => 'database',
            '_relationship_key' => 0,
        ];

        $tunnel = $this->createManager()->create($selection, $service, 30000);

        $this->assertSame(3306, $tunnel->remotePort);
        $this->assertSame(30000, $tunnel->localPort);
    }

    public function testUnserializeOldFormatDerivedIdIsStable(): void
    {
        $json = (string) json_encode([
            'key' => [
                'projectId' => 'proj2',
                'environmentId' => 'dev',
                'appName' => null,
                'relationship' => 'db',
                'serviceKey' => 0,
                'service' => ['scheme' => 'pgsql', 'host' => 'pg.internal', 'port' => 5432],
                'localPort' => 30002,
                'remoteHost' => 'pg.internal',
                'remotePort' => 5432,
                'pid' => null,
            ],
        ]);

        $manager = $this->createManager();
        $tunnels1 = $this->callUnserialize($manager, $json);
        $tunnels2 = $this->callUnserialize($manager, $json);

        $this->assertSame($tunnels1[0]->id, $tunnels2[0]->id);
        // Null appName becomes empty string in the ID.
        $this->assertSame('proj2--dev----db--0', $tunnels1[0]->id);
    }

    public function testIsOpenLoadsStateWhenNotAlreadyLoaded(): void
    {
        // create() with an explicit local port skips getPort(), which is the
        // only other caller of getTunnels(), so isOpen() has to load the state
        // itself. Regression test for CLI-169.
        $id = 'proj1--main--app--database--0';
        $metadata = [
            'projectId' => 'proj1',
            'environmentId' => 'main',
            'appName' => 'app',
            'relationship' => 'database',
            'serviceKey' => 0,
            'service' => ['scheme' => 'mysql', 'host' => 'database.internal', 'port' => 3306],
        ];
        $this->writeTunnelInfo($id, $metadata + [
            'localPort' => 30000,
            'remoteHost' => 'database.internal',
            'remotePort' => 3306,
            // Alive, so it survives the pruning in getTunnels().
            'pid' => getmypid(),
        ]);

        $manager = $this->createManager($this->tempDir);

        $result = $manager->isOpen(new Tunnel($id, 30000, 'database.internal', 3306, $metadata));

        $this->assertInstanceOf(Tunnel::class, $result);
        $this->assertSame(getmypid(), $result->pid);
    }

    public function testIsOpenPrunesTunnelWithoutPid(): void
    {
        // An entry with no PID is stale: getTunnels() drops it and rewrites the
        // state file, which a caller passing an explicit port would otherwise
        // skip, leaving isOpen() reporting a tunnel that is not there.
        $id = 'proj2--dev----redis--0';
        $metadata = [
            'projectId' => 'proj2',
            'environmentId' => 'dev',
            'appName' => null,
            'relationship' => 'redis',
            'serviceKey' => 0,
            'service' => ['scheme' => 'redis', 'host' => 'redis.internal', 'port' => 6379],
        ];
        $filename = $this->writeTunnelInfo($id, $metadata + [
            'localPort' => 30002,
            'remoteHost' => 'redis.internal',
            'remotePort' => 6379,
            'pid' => null,
        ]);

        $manager = $this->createManager($this->tempDir);

        $this->assertFalse($manager->isOpen(new Tunnel($id, 30002, 'redis.internal', 6379, $metadata)));
        $this->assertFileDoesNotExist($filename);
    }
}
