<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Io;
use Platformsh\Cli\Service\Relationships;
use Platformsh\Cli\Service\TunnelManager;
use Platformsh\Cli\Tunnel\Tunnel;

class TunnelManagerTest extends TestCase
{
    private function createManager(): TunnelManager
    {
        $config = $this->createMock(Config::class);
        $io = $this->createMock(Io::class);
        $relationships = $this->createMock(Relationships::class);

        return new TunnelManager($config, $io, $relationships);
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
}
