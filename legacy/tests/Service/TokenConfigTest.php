<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\TokenConfig;
use Symfony\Component\Filesystem\Filesystem;

class TokenConfigTest extends TestCase
{
    private string $home;

    public function setUp(): void
    {
        $this->home = sys_get_temp_dir() . '/token-config-test-' . bin2hex(random_bytes(4));
        mkdir($this->home);
    }

    public function tearDown(): void
    {
        (new Filesystem())->remove($this->home);
    }

    /**
     * @param array<string, mixed> $overrides
     */
    private function tokenConfig(array $overrides = []): TokenConfig
    {
        $config = new Config(['MOCK_CLI_HOME' => $this->home], dirname(__DIR__) . '/data/mock-cli-config.yaml');

        return new TokenConfig($config->withOverrides($overrides));
    }

    private function tokenFile(string $content): string
    {
        $file = $this->home . '/token';
        file_put_contents($file, $content);

        return $file;
    }

    public function testConfiguredTokenTakesPrecedence(): void
    {
        $this->tokenConfig()->storage()->storeToken('stored');

        $this->assertSame('stored', $this->tokenConfig()->getApiToken());
        $this->assertNull($this->tokenConfig()->getApiToken(false));
        $this->assertSame('from-config', $this->tokenConfig(['api.token' => 'from-config'])->getApiToken());
        $file = $this->tokenFile("from-file\n");
        $this->assertSame('from-file', $this->tokenConfig(['api.token_file' => $file])->getApiToken());
    }

    public function testEmptyTokenFileIsIgnored(): void
    {
        $this->tokenConfig()->storage()->storeToken('stored');
        $file = $this->tokenFile("\n");

        $this->assertSame('stored', $this->tokenConfig(['api.token_file' => $file])->getApiToken());
        $this->assertNull($this->tokenConfig(['api.token_file' => $file])->getApiToken(false));
    }
}
