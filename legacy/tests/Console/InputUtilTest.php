<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Console;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Console\InputUtil;
use Symfony\Component\Console\Exception\InvalidArgumentException;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;

class InputUtilTest extends TestCase
{
    /**
     * @param array<string, mixed> $params
     */
    private static function input(array $params, string|int|null $default = null): InputInterface
    {
        return new ArrayInput($params, new InputDefinition([
            new InputArgument('arg', InputArgument::OPTIONAL, '', $default),
            new InputOption('opt', null, InputOption::VALUE_REQUIRED, '', $default),
        ]));
    }

    public function testStringValues(): void
    {
        $input = self::input(['arg' => 'a', '--opt' => 'o']);
        $this->assertSame('a', InputUtil::getStringArgument($input, 'arg'));
        $this->assertSame('a', InputUtil::getNullableStringArgument($input, 'arg'));
        $this->assertSame('o', InputUtil::getStringOption($input, 'opt'));
        $this->assertSame('o', InputUtil::getNullableStringOption($input, 'opt'));
    }

    public function testNullValues(): void
    {
        $input = self::input([]);
        $this->assertNull(InputUtil::getNullableStringArgument($input, 'arg'));
        $this->assertNull(InputUtil::getNullableStringOption($input, 'opt'));
    }

    /**
     * @return array<string, array{callable(InputInterface): mixed, InputInterface}>
     */
    public static function invalidProvider(): array
    {
        $none = self::input([]);
        $int = self::input(['arg' => 1, '--opt' => 1]);

        return [
            'null string argument' => [fn(InputInterface $i) => InputUtil::getStringArgument($i, 'arg'), $none],
            'null string option' => [fn(InputInterface $i) => InputUtil::getStringOption($i, 'opt'), $none],
            'int string argument' => [fn(InputInterface $i) => InputUtil::getStringArgument($i, 'arg'), $int],
            'int nullable argument' => [fn(InputInterface $i) => InputUtil::getNullableStringArgument($i, 'arg'), $int],
            'int string option' => [fn(InputInterface $i) => InputUtil::getStringOption($i, 'opt'), $int],
            'int nullable option' => [fn(InputInterface $i) => InputUtil::getNullableStringOption($i, 'opt'), $int],
        ];
    }

    /**
     * @param callable(InputInterface): mixed $fn
     */
    #[DataProvider('invalidProvider')]
    public function testInvalidValues(callable $fn, InputInterface $input): void
    {
        $this->expectException(\LogicException::class);
        $fn($input);
    }

    public function testIntOption(): void
    {
        $this->assertSame(5, InputUtil::getIntOption(self::input(['--opt' => '5']), 'opt'));
        $this->assertSame(5, InputUtil::getIntOption(self::input(['--opt' => 5]), 'opt'));
        $this->assertSame(10, InputUtil::getIntOption(self::input([], 10), 'opt'));
    }

    public function testIntOptionInvalid(): void
    {
        foreach (['-1', 'abc', '1.5', ''] as $value) {
            try {
                InputUtil::getIntOption(self::input(['--opt' => $value]), 'opt');
                $this->fail('Expected exception for ' . $value);
            } catch (InvalidArgumentException $e) {
                $this->assertSame('The --opt value must be a non-negative integer.', $e->getMessage());
            }
        }
    }
}
