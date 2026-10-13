<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use Doctrine\Common\Cache\ArrayCache;
use GuzzleHttp\Client;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Client\Connection\ConnectorInterface;
use Platformsh\Client\Model\User;
use Platformsh\Client\PlatformClient;
use Platformsh\Client\Session\Session;
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

    public function testAnExpiredTokenIsRefreshedBeforeTheCacheIsUsed(): void
    {
        $session = new Session('user-cache-test');
        $connector = $this->createMock(ConnectorInterface::class);
        $connector->method('getSession')->willReturn($session);
        $client = $this->createMock(PlatformClient::class);
        $client->method('getConnector')->willReturn($connector);
        $api = $this->createApi($client, new ArrayCache(), 'user-a');

        $session->set('accessToken', $this->jwt(['sub' => 'user-a', 'exp' => time() + 600]));
        $api->userCache()->save('me', ['id' => 'user-a', 'username' => 'cached']);

        // The request refreshes the token, as the API client's middleware would.
        $session->set('accessToken', $this->jwt(['sub' => 'user-a', 'exp' => time() - 60]));
        $client->expects($this->once())
            ->method('getUser')
            ->with('me')
            ->willReturnCallback(function () use ($session): User {
                $session->set('accessToken', $this->jwt(['sub' => 'user-a', 'exp' => time() + 600]));
                return $this->user('user-a');
            });

        $this->assertSame('cached', $api->getUser()->getProperty('username'));
    }

    public function testAnExpiredTokenIsCheckedOnce(): void
    {
        // The API client does not refresh the token, e.g. if the local clock is ahead of the server's.
        $session = new Session('user-cache-test');
        $session->set('accessToken', $this->jwt(['sub' => 'user-a', 'exp' => time() - 60]));
        $connector = $this->createMock(ConnectorInterface::class);
        $connector->method('getSession')->willReturn($session);
        $client = $this->createMock(PlatformClient::class);
        $client->method('getConnector')->willReturn($connector);
        $client->expects($this->exactly(2))
            ->method('getUser')
            ->with('me')
            ->willReturn($this->user('user-a'));

        $api = $this->createApi($client, new ArrayCache(), 'user-a');
        $api->getUser();
        $api->getUser();
    }

    public function testARejectedExpiredTokenIsCheckedAgain(): void
    {
        $session = new Session('user-cache-test');
        $session->set('accessToken', $this->jwt(['sub' => 'user-a', 'exp' => time() - 60]));
        $connector = $this->createMock(ConnectorInterface::class);
        $connector->method('getSession')->willReturn($session);
        $client = $this->createMock(PlatformClient::class);
        $client->method('getConnector')->willReturn($connector);
        $client->expects($this->exactly(2))
            ->method('getUser')
            ->with('me')
            ->willThrowException(new \RuntimeException('Unauthorized'));

        $api = $this->createApi($client, new ArrayCache(), 'user-a');
        $rejections = 0;
        for ($i = 0; $i < 2; $i++) {
            try {
                $api->userCache();
            } catch (\RuntimeException) {
                $rejections++;
            }
        }
        $this->assertSame(2, $rejections);
    }

    /**
     * @param array<string, mixed> $claims
     */
    private function jwt(array $claims): string
    {
        return 'e30.' . base64_encode((string) json_encode($claims)) . '.signature';
    }

    private function user(string $id): User
    {
        return new User(['id' => $id, 'username' => 'fetched'], 'https://api.example.test/api/users', new Client());
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
