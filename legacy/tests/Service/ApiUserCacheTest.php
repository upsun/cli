<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use Doctrine\Common\Cache\ArrayCache;
use GuzzleHttp\Client;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Client\Model\User;
use Platformsh\Client\PlatformClient;
use Symfony\Component\Console\Output\BufferedOutput;

class ApiUserCacheTest extends TestCase
{
    public function testCachedDataIsNotSharedBetweenUsers(): void
    {
        $client = $this->createMock(PlatformClient::class);
        $client->expects($this->exactly(2))
            ->method('getUser')
            ->with('me')
            ->willReturnOnConsecutiveCalls($this->user('user-a'), $this->user('user-b'));

        $cache = new ArrayCache();
        $userIds = [];
        foreach (['user-a', 'user-a', 'user-b', 'user-a'] as $userId) {
            $userIds[] = $this->createApi($client, $cache, $userId)->getUser()->id;
        }

        $this->assertSame(['user-a', 'user-a', 'user-b', 'user-a'], $userIds);
    }

    private function user(string $id): User
    {
        return new User(['id' => $id], 'https://api.example.test/api/users', new Client());
    }

    private function createApi(PlatformClient $client, ArrayCache $cache, string $userId): Api
    {
        $config = new Config(['PLATFORMSH_CLI_SESSION_ID' => 'user-cache-test']);

        return new class ($client, $config, $cache, $userId) extends Api {
            public function __construct(private readonly PlatformClient $client, Config $config, ArrayCache $cache, private readonly string $userId)
            {
                parent::__construct($config, $cache, new BufferedOutput());
            }

            public function getClient(bool $autoLogin = true, bool $reset = false): PlatformClient
            {
                return $this->client;
            }

            public function getMyUserId(bool $reset = false): string
            {
                return $this->userId;
            }
        };
    }
}
