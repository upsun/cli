<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Model;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Model\Variable;

class VariableTest extends TestCase
{
    private string $invalidMessage = 'Variables must be defined as type:name=value';

    public function testParseValidVariables(): void
    {
        $this->assertEquals(
            ['env', 'foo', 'bar'],
            (new Variable())->parse('env:foo=bar'),
        );

        $this->assertEquals(
            ['env', 'foo:oof', 'bar'],
            (new Variable())->parse('env:foo:oof=bar'),
        );

        $this->assertEquals(
            ['env', 'foo.123', 'bar'],
            (new Variable())->parse('env:foo.123=bar'),
        );

        $this->assertEquals(
            ['complex', 'json', '{"foo":"bar"}'],
            (new Variable())->parse('complex:json={"foo":"bar"}'),
        );

        $this->assertEquals(
            ['empty', 'value', ''],
            (new Variable())->parse('empty:value='),
        );
    }

    public function testParseMultiple(): void
    {
        $this->assertEquals([], (new Variable())->parseMultiple([]));

        $this->assertEquals(
            [
                'env' => ['foo' => 'bar', 'baz' => 'qux'],
                'complex' => ['json' => '{"a":"b"}'],
            ],
            (new Variable())->parseMultiple([
                'env:foo=bar',
                'env:baz=qux',
                'complex:json={"a":"b"}',
            ]),
        );

        // A later definition overwrites an earlier one with the same type and name.
        $this->assertEquals(
            ['env' => ['foo' => 'second']],
            (new Variable())->parseMultiple(['env:foo=first', 'env:foo=second']),
        );
    }

    public function testParseMultipleWithInvalidVariable(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage($this->invalidMessage);
        (new Variable())->parseMultiple(['env:foo=bar', 'invalid']);
    }

    public function testParseInvalidVariableType(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage('Invalid variable type');
        (new Variable())->parse('a/b:c=d');
    }


    public function testParseInvalidVariableName(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage('Invalid variable name');
        (new Variable())->parse('a:b(c)=d');
    }

    public function testParseVariableWithNoDelimiter(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage($this->invalidMessage);
        (new Variable())->parse('foo');
    }

    public function testParseVariableWithWrongDelimiterOrder(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage($this->invalidMessage);
        (new Variable())->parse('a=b:c');
    }

    public function testParseVariableWithEmptyType(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage($this->invalidMessage);
        (new Variable())->parse(':b=c');
    }
}
