<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Service;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Service\QuestionHelper;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Output\BufferedOutput;

class QuestionHelperTest extends TestCase
{
    /**
     * @return array<string, array{array<array-key, string>, string, ?string, string}>
     */
    public static function chooseAssocProvider(): array
    {
        return [
            'string key' => [['a' => 'Apple', 'b' => 'Banana'], "b\n", null, 'b'],
            'string value' => [['a' => 'Apple', 'b' => 'Banana'], "Banana\n", null, 'b'],
            'string default' => [['a' => 'Apple', 'b' => 'Banana'], "\n", 'b', 'b'],
            'integer key' => [['1' => 'One', '2' => 'Two'], "2\n", null, '2'],
            'integer value' => [['1' => 'One', '2' => 'Two'], "Two\n", null, '2'],
            'integer default' => [['1' => 'One', '2' => 'Two'], "\n", '2', '2'],
            'key before value' => [['1' => '2', '2' => 'Two'], "2\n", null, '2'],
        ];
    }

    /**
     * @param array<array-key, string> $items
     */
    #[DataProvider('chooseAssocProvider')]
    public function testChooseAssoc(array $items, string $answer, ?string $default, string $expected): void
    {
        $stream = fopen('php://memory', 'r+');
        $this->assertNotFalse($stream);
        fwrite($stream, $answer);
        rewind($stream);
        $input = new ArrayInput([]);
        $input->setStream($stream);

        $helper = new QuestionHelper($input, new BufferedOutput());
        $this->assertSame($expected, $helper->chooseAssoc($items, 'Choose:', $default));
    }

    public function testChooseAssocNonInteractive(): void
    {
        $input = new ArrayInput([]);
        $input->setInteractive(false);

        $helper = new QuestionHelper($input, new BufferedOutput());
        $this->assertSame('2', $helper->chooseAssoc(['1' => 'One', '2' => 'Two'], 'Choose:', '2'));
    }
}
