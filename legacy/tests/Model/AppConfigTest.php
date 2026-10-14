<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Model;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Model\AppConfig;

class AppConfigTest extends TestCase
{
    /**
     * @return array<string, array{array<string, mixed>, array<string, mixed>|null}>
     */
    public static function phpRuntimeProvider(): array
    {
        $xdebug = ['xdebug' => ['idekey' => 'secret']];

        return [
            'php type' => [['type' => 'php:8.4', 'runtime' => $xdebug], $xdebug],
            'php type without runtime' => [['type' => 'php:8.4'], []],
            'non-php type' => [['type' => 'nodejs:22', 'runtime' => $xdebug], null],
            'no type' => [[], null],
            'composable, runtime list with mapping' => [
                ['type' => 'composable:25.05', 'stack' => ['runtimes' => ['nodejs@22', ['php@8.4' => $xdebug]]]],
                $xdebug,
            ],
            'composable, runtime list with string' => [
                ['type' => 'composable:25.05', 'stack' => ['runtimes' => ['php@8.4']]],
                [],
            ],
            'composable, runtime mapping' => [
                ['type' => 'composable:25.05', 'stack' => ['runtimes' => ['php@8.4' => $xdebug]]],
                $xdebug,
            ],
            'composable, runtime string' => [
                ['type' => 'composable:25.05', 'stack' => ['runtimes' => 'php@8.4']],
                [],
            ],
            'composable, runtime with null config' => [
                ['type' => 'composable:25.05', 'stack' => ['runtimes' => [['php@8.4' => null]]]],
                [],
            ],
            'composable without php' => [
                ['type' => 'composable:25.05', 'stack' => ['runtimes' => ['nodejs@22'], 'packages' => ['php']]],
                null,
            ],
            'composable without stack' => [['type' => 'composable:25.05'], null],
        ];
    }

    /**
     * @param array<string, mixed> $config
     * @param array<string, mixed>|null $expected
     */
    #[DataProvider('phpRuntimeProvider')]
    public function testGetPhpRuntime(array $config, ?array $expected): void
    {
        $this->assertSame($expected, (new AppConfig($config))->getPhpRuntime());
    }
}
