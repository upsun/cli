<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Command\SshKey;

use PHPUnit\Framework\MockObject\MockObject;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Command\SshKey\SshKeyAddCommand;
use Platformsh\Cli\Model\SshKey as SshKeyModel;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\Io;
use Platformsh\Cli\Service\QuestionHelper;
use Platformsh\Cli\Service\Shell;
use Platformsh\Cli\Service\SshConfig;
use Platformsh\Cli\Service\SshKey;
use Symfony\Component\Console\Tester\CommandTester;

class SshKeyAddCommandTest extends TestCase
{
    private string $publicKeyPath;

    protected function setUp(): void
    {
        $this->publicKeyPath = (string) tempnam(sys_get_temp_dir(), 'ssh-key-add-test');
        file_put_contents($this->publicKeyPath, 'ssh-ed25519 AAAA');
    }

    protected function tearDown(): void
    {
        unlink($this->publicKeyPath);
    }

    public function testDuplicateCheckBypassesTheCache(): void
    {
        $api = $this->createApi();
        $api->expects($this->atLeastOnce())
            ->method('getSshKeys')
            ->with(true)
            ->willReturn([]);
        $api->expects($this->once())->method('addSshKey');

        $tester = new CommandTester($this->createCommand($api));

        $this->assertSame(0, $tester->execute(['path' => $this->publicKeyPath]));
    }

    public function testExistingInactiveKeyIsReported(): void
    {
        $api = $this->createApi();
        $api->method('getSshKeys')->willReturn([$this->createKey(false)]);
        $api->expects($this->never())->method('addSshKey');

        $tester = new CommandTester($this->createCommand($api));

        $this->assertSame(1, $tester->execute(['path' => $this->publicKeyPath]));
        $this->assertStringContainsString('inactive', $tester->getDisplay());
    }

    public function testExistingActiveKeyIsNotAddedAgain(): void
    {
        $api = $this->createApi();
        $api->method('getSshKeys')->willReturn([$this->createKey(true)]);
        $api->expects($this->never())->method('addSshKey');

        $tester = new CommandTester($this->createCommand($api));

        $this->assertSame(0, $tester->execute(['path' => $this->publicKeyPath]));
        $this->assertStringContainsString('This key already exists in your account.', $tester->getDisplay());
    }

    private function createCommand(Api $api): SshKeyAddCommand
    {
        $questionHelper = $this->createMock(QuestionHelper::class);
        $questionHelper->method('confirm')->willReturn(true);
        $sshKey = $this->createMock(SshKey::class);
        $sshKey->method('getPublicKeyFingerprint')->willReturn('SHA256:fingerprint');

        return new SshKeyAddCommand(
            $api,
            new Config(),
            $this->createMock(Io::class),
            $questionHelper,
            $this->createMock(Shell::class),
            $this->createMock(SshConfig::class),
            $sshKey,
        );
    }

    private function createApi(): Api&MockObject
    {
        $api = $this->createMock(Api::class);
        $api->method('getMyAccount')->willReturn(['email' => 'user@example.com']);

        return $api;
    }

    private function createKey(bool $active): SshKeyModel
    {
        return new SshKeyModel(
            'key-id',
            'SHA256:fingerprint',
            'ssh-ed25519 AAAA',
            'label',
            $active,
            'user-id',
            '2026-01-01T00:00:00Z',
            '2026-01-01T00:00:00Z',
        );
    }
}
