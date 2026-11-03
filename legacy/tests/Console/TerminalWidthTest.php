<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Console;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Console\TerminalWidth;

class TerminalWidthTest extends TestCase
{
    private string|false $columns;

    protected function setUp(): void
    {
        $this->columns = getenv('COLUMNS');
        $this->resetCapture();
    }

    protected function tearDown(): void
    {
        putenv($this->columns === false ? 'COLUMNS' : 'COLUMNS=' . $this->columns);
        $this->resetCapture();
    }

    private function resetCapture(): void
    {
        (new \ReflectionProperty(TerminalWidth::class, 'columnsFromUser'))->setValue(null, null);
    }

    public function testUsesColumnsSetBeforeStartup(): void
    {
        putenv('COLUMNS=123');
        TerminalWidth::captureEnv();
        $this->assertSame(123, FakeTerminalWidth::get());
    }

    public function testIgnoresColumnsSetAfterStartup(): void
    {
        putenv('COLUMNS');
        TerminalWidth::captureEnv();
        // Symfony's Application exports the width on startup.
        putenv('COLUMNS=123');
        $this->assertSame(150, FakeTerminalWidth::get());
    }

    public function testIgnoresColumnsSetBeforeNestedRun(): void
    {
        putenv('COLUMNS');
        TerminalWidth::captureEnv();
        putenv('COLUMNS=123');
        TerminalWidth::captureEnv();
        $this->assertSame(150, FakeTerminalWidth::get());
    }

    public function testFallsBackWithoutTerminal(): void
    {
        putenv('COLUMNS');
        TerminalWidth::captureEnv();
        putenv('COLUMNS=123');
        FakeTerminalWidth::$width = null;
        try {
            $this->assertSame(123, FakeTerminalWidth::get());
        } finally {
            FakeTerminalWidth::$width = 150;
        }
    }

    public function testParseSttySize(): void
    {
        $this->assertSame(150, TerminalWidth::parseSttySize("40 150\n"));
        $this->assertNull(TerminalWidth::parseSttySize("0 0\n"));
        $this->assertNull(TerminalWidth::parseSttySize(''));
    }
}

class FakeTerminalWidth extends TerminalWidth
{
    public static ?int $width = 150;

    protected static function readWidth(): ?int
    {
        return self::$width;
    }
}
