<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\SshCert;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\SshCert\Certifier;
use Platformsh\Cli\Tests\HasTempDirTrait;

class CertifierTest extends TestCase
{
    use HasTempDirTrait;

    public function testResolveKeyAlgorithm(): void
    {
        $this->tempDirSetUp();
        assert($this->tempDir !== null);
        $fipsOn = $this->tempDir . '/fips-on';
        $fipsOff = $this->tempDir . '/fips-off';
        $missing = $this->tempDir . '/missing';
        file_put_contents($fipsOn, "1\n");
        file_put_contents($fipsOff, "0\n");

        $cases = [
            ['auto', $fipsOn, 'rsa'],
            ['auto', $fipsOff, 'ed25519'],
            ['auto', $missing, 'ed25519'],
            ['', $fipsOn, 'rsa'],
            ['', $missing, 'ed25519'],
            ['ed25519', $fipsOn, 'ed25519'],
            ['rsa', $fipsOff, 'rsa'],
        ];
        foreach ($cases as [$configured, $fipsFile, $expected]) {
            $this->assertSame($expected, Certifier::resolveKeyAlgorithm($configured, $fipsFile), sprintf('configured "%s" with %s', $configured, basename($fipsFile)));
        }
    }

    public function testResolveKeyAlgorithmRejectsInvalid(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        Certifier::resolveKeyAlgorithm('../rsa', '/nonexistent');
    }
}
