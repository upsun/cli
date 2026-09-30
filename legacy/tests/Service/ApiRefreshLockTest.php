<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use Doctrine\Common\Cache\ArrayCache;
use League\OAuth2\Client\Token\AccessToken;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\FileLock;
use Platformsh\Cli\Tests\HasTempDirTrait;
use Platformsh\Cli\Tests\LockHolderTrait;
use Platformsh\Client\Connection\Connector;
use Platformsh\Client\Session\Storage\File;
use Symfony\Component\Console\Output\BufferedOutput;

class ApiRefreshLockTest extends TestCase
{
    use HasTempDirTrait;
    use LockHolderTrait;

    /** @var resource|null */
    private $tokenServer = null;

    private File $storage;

    public function setUp(): void
    {
        $this->tempDirSetUp();
        $this->storage = new File($this->config()->getSessionDir());
    }

    public function tearDown(): void
    {
        // Api caches the client statically: do not leak this session into other tests.
        (new \ReflectionProperty(Api::class, 'client'))->setValue(null, null);
        if ($this->tokenServer !== null) {
            \proc_terminate($this->tokenServer);
            \proc_close($this->tokenServer);
        }
    }

    public function testUsesTheStoredTokenWhenTheLockIsFree(): void
    {
        $onRefreshStart = $this->loadStaleSession($this->config());

        $token = $onRefreshStart('refresh-1');

        $this->assertInstanceOf(AccessToken::class, $token);
        $this->assertSame('access-2', $token->getToken());
        $this->assertSame('refresh-2', $token->getRefreshToken());
    }

    public function testUsesTheStoredTokenAfterWaiting(): void
    {
        $config = $this->config();
        $onRefreshStart = $this->loadStaleSession($config);
        $holder = $this->startLockHolder((string) $this->tempDir, 'refresh--' . $config->getSessionIdSlug(), 1);

        $token = $onRefreshStart('refresh-1');
        \proc_close($holder);

        $this->assertInstanceOf(AccessToken::class, $token);
        $this->assertSame('refresh-2', $token->getRefreshToken());
    }

    public function testSavesTheRefreshedTokenBeforeReturning(): void
    {
        $config = $this->config(['PLATFORMSH_CLI_OAUTH2_TOKEN_URL' => $this->startTokenServer()]);
        $this->storage->save('refresh-test', $this->sessionData('access-1', 'refresh-1'));
        $onRefreshStart = $this->onRefreshStart($config);

        $token = $onRefreshStart('refresh-1');

        // The token must be saved before on_refresh_end releases the lock.
        $this->assertInstanceOf(AccessToken::class, $token);
        $this->assertSame('refresh-2', $token->getRefreshToken());
        $this->assertSame('refresh-2', $this->storage->load('refresh-test')['refreshToken'] ?? null);
    }

    public function testFailsWithoutRefreshingAfterTimingOut(): void
    {
        // The token URL is unreachable, so any refresh attempt fails differently.
        $config = $this->config(['PLATFORMSH_CLI_OAUTH2_TOKEN_URL' => 'http://127.0.0.1:1/oauth2/token']);
        $this->storage->save('refresh-test', $this->sessionData('access-1', 'refresh-1'));
        $onRefreshStart = $this->onRefreshStart($config, new FileLock($config, 1));
        $holder = $this->startLockHolder((string) $this->tempDir, 'refresh--' . $config->getSessionIdSlug());

        try {
            $this->expectExceptionMessage('Timed out waiting for another process to refresh the access token');
            $onRefreshStart('refresh-1');
        } finally {
            \proc_terminate($holder, 9);
            \proc_close($holder);
        }
    }

    /**
     * @param array<string, string> $env
     */
    private function config(array $env = []): Config
    {
        return new Config($env + [
            'PLATFORMSH_CLI_HOME' => (string) $this->tempDir,
            'PLATFORMSH_CLI_SESSION_ID' => 'refresh-test',
            'PLATFORMSH_CLI_API_DISABLE_CREDENTIAL_HELPERS' => '1',
        ]);
    }

    /**
     * Loads the session in memory, then simulates another process refreshing the token.
     */
    private function loadStaleSession(Config $config): callable
    {
        $this->storage->save('refresh-test', $this->sessionData('access-1', 'refresh-1'));
        $onRefreshStart = $this->onRefreshStart($config);
        $this->storage->save('refresh-test', $this->sessionData('access-2', 'refresh-2'));

        return $onRefreshStart;
    }

    private function onRefreshStart(Config $config, ?FileLock $fileLock = null): callable
    {
        $api = new Api($config, new ArrayCache(), new BufferedOutput(), null, null, $fileLock);
        $connector = $api->getClient(false, true)->getConnector();
        $this->assertInstanceOf(Connector::class, $connector);
        $this->assertSame('refresh-1', $connector->getSession()->get('refreshToken'));

        $onRefreshStart = $connector->getConfig()['on_refresh_start'];
        $this->assertIsCallable($onRefreshStart);

        return $onRefreshStart;
    }

    /**
     * Starts a mock token endpoint, and returns its URL.
     */
    private function startTokenServer(): string
    {
        $socket = \stream_socket_server('tcp://127.0.0.1:0');
        $this->assertIsResource($socket);
        $address = (string) \stream_socket_get_name($socket, false);
        \fclose($socket);
        $port = (int) \substr($address, (int) \strrpos($address, ':') + 1);

        $router = \dirname(__DIR__) . '/data/oauth2-token-router.php';
        $process = \proc_open([\PHP_BINARY, '-S', $address, $router], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
        $this->assertIsResource($process);
        $this->tokenServer = $process;

        for ($i = 0; $i < 50; $i++) {
            $connection = @\fsockopen('127.0.0.1', $port);
            if ($connection) {
                \fclose($connection);
                return 'http://' . $address . '/oauth2/token';
            }
            \usleep(100_000);
        }
        $this->fail('The token server did not start');
    }

    /**
     * @return array<string, mixed>
     */
    private function sessionData(string $accessToken, string $refreshToken): array
    {
        return [
            'accessToken' => $accessToken,
            'tokenType' => 'bearer',
            'expires' => \time() + 900,
            'refreshToken' => $refreshToken,
        ];
    }
}
