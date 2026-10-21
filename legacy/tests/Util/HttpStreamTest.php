<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Util;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Tests\HttpServerTrait;
use Platformsh\Cli\Util\HttpStream;

class HttpStreamTest extends TestCase
{
    use HttpServerTrait;

    public function testReadsTheBodyAsItArrives(): void
    {
        $stream = new HttpStream(self::$httpServerUrl . '/stream', [], .1);
        $this->assertTrue($stream->start(5));

        $reads = [];
        $deadline = microtime(true) + 5;
        while (!$stream->eof() && microtime(true) < $deadline) {
            $content = $stream->read();
            if ($content !== '') {
                $reads[] = $content;
            }
        }
        $this->assertTrue($stream->eof());
        $this->assertEquals(['one', 'two'], $reads, 'each part is read when it arrives');
    }

    public function testReadWaitsForALimitedTime(): void
    {
        $stream = new HttpStream(self::$httpServerUrl . '/slow', [], .1);
        $stream->start(.2);
        $start = microtime(true);
        $this->assertSame('', $stream->read());
        $this->assertLessThan(1, microtime(true) - $start);
    }

    public function testStartFailsOnAnErrorStatus(): void
    {
        $stream = new HttpStream(self::$httpServerUrl . '/missing', [], .1);
        $this->assertFalse($stream->start(5));
        $this->assertTrue($stream->eof());
        $this->assertSame('', $stream->read());
    }

    public function testStartTimesOut(): void
    {
        $stream = new HttpStream(self::$httpServerUrl . '/slow', [], .1);
        $start = microtime(true);
        $this->assertFalse($stream->start(.3));
        $this->assertLessThan(2, microtime(true) - $start);
        $this->assertTrue($stream->eof());
    }
}
