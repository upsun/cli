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
}
