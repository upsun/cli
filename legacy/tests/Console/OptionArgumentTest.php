<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Console;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Console\Argument;
use Platformsh\Cli\Console\Option;
use Symfony\Component\Console\Exception\InvalidArgumentException;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;

class OptionArgumentTest extends TestCase
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
        $this->assertSame('a', Argument::string($input, 'arg'));
        $this->assertSame('a', Argument::stringOrNull($input, 'arg'));
        $this->assertSame('o', Option::string($input, 'opt'));
        $this->assertSame('o', Option::stringOrNull($input, 'opt'));
    }

    public function testNullValues(): void
    {
        $input = self::input([]);
        $this->assertNull(Argument::stringOrNull($input, 'arg'));
        $this->assertNull(Option::stringOrNull($input, 'opt'));
    }

    /**
     * @return array<string, array{callable(InputInterface): mixed, InputInterface}>
     */
    public static function invalidProvider(): array
    {
        $none = self::input([]);
        $int = self::input(['arg' => 1, '--opt' => 1]);

        return [
            'null string argument' => [fn(InputInterface $i) => Argument::string($i, 'arg'), $none],
            'null string option' => [fn(InputInterface $i) => Option::string($i, 'opt'), $none],
            'int string argument' => [fn(InputInterface $i) => Argument::string($i, 'arg'), $int],
            'int nullable argument' => [fn(InputInterface $i) => Argument::stringOrNull($i, 'arg'), $int],
            'int string option' => [fn(InputInterface $i) => Option::string($i, 'opt'), $int],
            'int nullable option' => [fn(InputInterface $i) => Option::stringOrNull($i, 'opt'), $int],
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

    public function testStringArrayValues(): void
    {
        $input = new ArrayInput(['arr' => ['a', 'b'], '--arr' => ['c']], new InputDefinition([
            new InputArgument('arr', InputArgument::IS_ARRAY),
            new InputOption('arr', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY),
        ]));
        $this->assertSame(['a', 'b'], Argument::stringArray($input, 'arr'));
        $this->assertSame(['c'], Option::stringArray($input, 'arr'));
    }

    public function testStringArrayInvalid(): void
    {
        $this->expectException(\LogicException::class);
        Option::stringArray(self::input(['--opt' => 'a']), 'opt');
    }

    public function testStringArrayInvalidItem(): void
    {
        $input = new ArrayInput(['--arr' => ['a', 1]], new InputDefinition([
            new InputOption('arr', null, InputOption::VALUE_REQUIRED | InputOption::VALUE_IS_ARRAY),
        ]));
        $this->expectException(\LogicException::class);
        Option::stringArray($input, 'arr');
    }

    public function testIntOption(): void
    {
        $this->assertSame(5, Option::int(self::input(['--opt' => '5']), 'opt'));
        $this->assertSame(5, Option::int(self::input(['--opt' => 5]), 'opt'));
        $this->assertSame(10, Option::int(self::input([], 10), 'opt'));
    }

    public function testIntOrNullOption(): void
    {
        $this->assertNull(Option::intOrNull(self::input([]), 'opt'));
        $this->assertSame(5, Option::intOrNull(self::input(['--opt' => '5']), 'opt'));
        $this->expectException(InvalidArgumentException::class);
        Option::intOrNull(self::input(['--opt' => 'abc']), 'opt');
    }

    public function testIntOptionInvalid(): void
    {
        foreach (['-1', 'abc', '1.5', ''] as $value) {
            try {
                Option::int(self::input(['--opt' => $value]), 'opt');
                $this->fail('Expected exception for ' . $value);
            } catch (InvalidArgumentException $e) {
                $this->assertSame('The --opt value must be a non-negative integer.', $e->getMessage());
            }
        }
    }
}
