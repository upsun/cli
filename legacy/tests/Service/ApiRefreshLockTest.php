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
use Platformsh\Client\Connection\Connector;
use Platformsh\Client\Session\Storage\File;
use Symfony\Component\Console\Output\BufferedOutput;

class ApiRefreshLockTest extends TestCase
{
    use HasTempDirTrait;

    private Config $config;

    private File $storage;

    public function setUp(): void
    {
        $this->tempDirSetUp();
        $this->config = new Config([
            'PLATFORMSH_CLI_HOME' => (string) $this->tempDir,
            'PLATFORMSH_CLI_SESSION_ID' => 'refresh-test',
            'PLATFORMSH_CLI_API_DISABLE_CREDENTIAL_HELPERS' => '1',
        ]);
        $this->storage = new File($this->config->getSessionDir());
    }

    public function tearDown(): void
    {
        // Api caches the client statically: do not leak this session into other tests.
        (new \ReflectionProperty(Api::class, 'client'))->setValue(null, null);
    }

    public function testUsesTheStoredTokenWhenTheLockIsFree(): void
    {
        $onRefreshStart = $this->loadStaleSession();

        $token = $onRefreshStart('refresh-1');

        $this->assertInstanceOf(AccessToken::class, $token);
        $this->assertSame('access-2', $token->getToken());
        $this->assertSame('refresh-2', $token->getRefreshToken());
    }

    public function testUsesTheStoredTokenAfterWaiting(): void
    {
        $onRefreshStart = $this->loadStaleSession();

        // Another process holds the lock.
        $otherLock = new FileLock($this->config);
        $this->assertNull($otherLock->acquireOrWait('refresh--' . $this->config->getSessionIdSlug()));

        $token = $onRefreshStart('refresh-1');

        $this->assertInstanceOf(AccessToken::class, $token);
        $this->assertSame('refresh-2', $token->getRefreshToken());
    }

    public function testAllowsARefreshWhenTheStoredTokenIsUnchanged(): void
    {
        $this->storage->save('refresh-test', $this->sessionData('access-1', 'refresh-1'));
        $onRefreshStart = $this->onRefreshStart();

        $this->assertNull($onRefreshStart('refresh-1'));
    }

    /**
     * Loads the session in memory, then simulates another process refreshing the token.
     */
    private function loadStaleSession(): callable
    {
        $this->storage->save('refresh-test', $this->sessionData('access-1', 'refresh-1'));
        $onRefreshStart = $this->onRefreshStart();
        $this->storage->save('refresh-test', $this->sessionData('access-2', 'refresh-2'));

        return $onRefreshStart;
    }

    private function onRefreshStart(): callable
    {
        $api = new Api($this->config, new ArrayCache(), new BufferedOutput());
        $connector = $api->getClient(false, true)->getConnector();
        $this->assertInstanceOf(Connector::class, $connector);
        $this->assertSame('refresh-1', $connector->getSession()->get('refreshToken'));

        $onRefreshStart = $connector->getConfig()['on_refresh_start'];
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
            'expires' => time() + 900,
            'refreshToken' => $refreshToken,
        ];
    }
}
