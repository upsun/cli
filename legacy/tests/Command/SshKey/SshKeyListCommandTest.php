<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Command\SshKey;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Command\SshKey\SshKeyListCommand;
use Platformsh\Cli\Model\SshKey as SshKeyModel;
use Platformsh\Cli\Service\Api;
use Platformsh\Cli\Service\Config;
use Platformsh\Cli\Service\SshKey;
use Platformsh\Cli\Service\Table;
use Symfony\Component\Console\Tester\CommandTester;

class SshKeyListCommandTest extends TestCase
{
    public function testInactiveKeyShowsItsStatusAndLocalPath(): void
    {
        $key = new SshKeyModel(
            'key-id',
            'SHA256:fingerprint',
            'ssh-ed25519 AAAA',
            'inactive key',
            false,
            'user-id',
            '2026-01-01T00:00:00Z',
            '2026-01-01T00:00:00Z',
        );
        $api = $this->createMock(Api::class);
        $api->method('getSshKeys')->willReturn([$key]);

        $sshKey = $this->createMock(SshKey::class);
        $sshKey->expects($this->once())
            ->method('findIdentityMatchingPublicKeys')
            ->with([$key->sha256])
            ->willReturn('/home/test/.ssh/custom');

        $table = $this->createMock(Table::class);
        $table->method('formatIsMachineReadable')->willReturn(false);
        $table->expects($this->once())
            ->method('render')
            ->with(
                [[
                    'id' => 'key-id',
                    'label' => 'inactive key',
                    'sha256' => 'SHA256:fingerprint',
                    'active' => 'No',
                    'path' => '/home/test/.ssh/custom.pub',
                ]],
                $this->arrayHasKey('active'),
                ['id', 'label', 'active', 'path'],
            );

        $command = new SshKeyListCommand($api, new Config(), $sshKey, $table);
        $tester = new CommandTester($command);

        $this->assertSame(0, $tester->execute([]));
    }
}
