<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use Doctrine\Common\Cache\ArrayCache;
use League\OAuth2\Client\Token\AccessToken;
use PHPUnit\Framework\Attributes\DataProvider;
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

    public function testAllowsARefreshWhenTheStoredTokenIsUnchanged(): void
    {
        $config = $this->config();
        $this->storage->save('refresh-test', $this->sessionData('access-1', 'refresh-1'));
        ['on_refresh_start' => $onRefreshStart, 'on_refresh_end' => $onRefreshEnd] = $this->connector($config)->getConfig();
        $this->assertIsCallable($onRefreshStart);
        $this->assertIsCallable($onRefreshEnd);
        $lockName = 'refresh--' . $config->getSessionIdSlug();
        $otherProcess = new FileLock($config, 1);

        // The middleware refreshes while the lock is held.
        $this->assertNull($onRefreshStart('refresh-1'));
        $otherProcess->acquireOrWait($lockName);
        $this->assertFalse($otherProcess->isHeld($lockName));

        $onRefreshEnd('refresh-1');
        $otherProcess->acquireOrWait($lockName);
        $this->assertTrue($otherProcess->isHeld($lockName));
    }

    /**
     * @return array<string, array{bool}>
     */
    public static function timeoutCases(): array
    {
        return [
            'unchanged stored token' => [false],
            // The lock holder may be about to save an even newer token.
            'newer stored token' => [true],
        ];
    }

    #[DataProvider('timeoutCases')]
    public function testFailsAfterTimingOut(bool $storedTokenChanged): void
    {
        $config = $this->config();
        $this->storage->save('refresh-test', $this->sessionData('access-1', 'refresh-1'));
        $onRefreshStart = $this->onRefreshStart($config, new FileLock($config, 1));
        if ($storedTokenChanged) {
            $this->storage->save('refresh-test', $this->sessionData('access-2', 'refresh-2'));
        }
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

    private function connector(Config $config, ?FileLock $fileLock = null): Connector
    {
        $api = new Api($config, new ArrayCache(), new BufferedOutput(), null, null, $fileLock);
        $connector = $api->getClient(false, true)->getConnector();
        $this->assertInstanceOf(Connector::class, $connector);
        $this->assertSame('refresh-1', $connector->getSession()->get('refreshToken'));

        return $connector;
    }

    private function onRefreshStart(Config $config, ?FileLock $fileLock = null): callable
    {
        $onRefreshStart = $this->connector($config, $fileLock)->getConfig()['on_refresh_start'];
        $this->assertIsCallable($onRefreshStart);

        return $onRefreshStart;
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
