<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\SshCert;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\SshCert\Certifier;

class CertifierTest extends TestCase
{
    /**
     * @return array<string, array{string, bool, string}>
     */
    public static function keyAlgorithmProvider(): array
    {
        return [
            'auto without FIPS' => ['auto', false, 'ed25519'],
            'auto with FIPS' => ['auto', true, 'rsa'],
            'empty without FIPS' => ['', false, 'ed25519'],
            'empty with FIPS' => ['', true, 'rsa'],
            'explicit rsa without FIPS' => ['rsa', false, 'rsa'],
            'explicit rsa with FIPS' => ['rsa', true, 'rsa'],
            'explicit ed25519 with FIPS' => ['ed25519', true, 'ed25519'],
        ];
    }

    #[DataProvider('keyAlgorithmProvider')]
    public function testChooseKeyAlgorithm(string $configured, bool $fipsEnabled, string $expected): void
    {
        $this->assertSame($expected, Certifier::chooseKeyAlgorithm($configured, $fipsEnabled));
    }

    public function testChooseKeyAlgorithmTrimsWhitespace(): void
    {
        $this->assertSame('rsa', Certifier::chooseKeyAlgorithm(' rsa ', false));
        $this->assertSame('ed25519', Certifier::chooseKeyAlgorithm("auto\n", false));
    }

    /**
     * @return array<string, array{string}>
     */
    public static function invalidKeyAlgorithmProvider(): array
    {
        return [
            'path traversal' => ['../../.ssh/id_rsa'],
            'slash' => ['rsa/evil'],
            'space inside' => ['rsa evil'],
            'uppercase' => ['RSA'],
        ];
    }

    #[DataProvider('invalidKeyAlgorithmProvider')]
    public function testChooseKeyAlgorithmRejectsInvalidValues(string $configured): void
    {
        $this->expectException(\InvalidArgumentException::class);
        Certifier::chooseKeyAlgorithm($configured, false);
    }
}
