<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Model\SshKey as SshKeyModel;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\SshKey;
use Platformsh\Cli\Tests\Container;
use Platformsh\Cli\Tests\HasTempDirTrait;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\OutputInterface;

class SshKeyTest extends TestCase
{
    use HasTempDirTrait;

    private SshKey $sshKey;

    public function setUp(): void
    {
        $container = Container::instance();
        $container->set(InputInterface::class, new ArrayInput([]));
        $container->set(OutputInterface::class, new BufferedOutput());
        $container->set(Config::class, new Config());
        $sshKey = $container->get(SshKey::class);
        \assert($sshKey instanceof SshKey);
        $this->sshKey = $sshKey;
        $this->tempDirSetUp();
    }

    /**
     * The API reports SHA-256 fingerprints in OpenSSH's format, so the local
     * ones must match, or no local identity will ever be matched to an account.
     */
    public function testGetPublicKeyFingerprint(): void
    {
        // An ed25519 public key and its fingerprint, as reported by ssh-keygen.
        $value = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL4dwyaTPzTnnLbCTIU3TzT/qFtCG7SFwBnGXbFtsuKX test@example.com';
        $expected = 'SHA256:Zc8rf0C3ZFAVs8mnWl4r6jKmJN8kutsoBK2h4UkXPp8';

        $path = $this->tempDir . '/id_ed25519.pub';
        file_put_contents($path, $value . "\n");

        $this->assertEquals($expected, $this->sshKey->getPublicKeyFingerprint($path));
    }

    public function testGetPublicKeyFingerprintFailsOnInvalidKey(): void
    {
        $path = $this->tempDir . '/invalid.pub';
        file_put_contents($path, 'not-a-key');

        $this->expectException(\RuntimeException::class);
        $this->sshKey->getPublicKeyFingerprint($path);
    }

    public function testInactiveAccountKeysAreNotMatched(): void
    {
        $sshDir = $this->tempDir . '/.ssh';
        mkdir($sshDir);
        $value = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL4dwyaTPzTnnLbCTIU3TzT/qFtCG7SFwBnGXbFtsuKX test@example.com';
        file_put_contents($sshDir . '/custom.pub', $value . "\n");
        file_put_contents($sshDir . '/custom', 'private key placeholder');

        $api = $this->createMock(Api::class);
        $api->method('getSshKeys')->willReturn([
            new SshKeyModel(
                'key-id',
                'SHA256:Zc8rf0C3ZFAVs8mnWl4r6jKmJN8kutsoBK2h4UkXPp8',
                $value,
                'inactive key',
                false,
                'user-id',
                '2026-01-01T00:00:00Z',
                '2026-01-01T00:00:00Z',
            ),
        ]);
        $config = new Config(['PLATFORMSH_CLI_HOME' => (string) $this->tempDir]);
        $this->assertSame($this->tempDir, $config->getHomeDirectory());
        $service = new SshKey($config, $api, new BufferedOutput());

        $this->assertFalse($service->hasLocalKey());
    }
}
