<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Util;

use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Util\CurlStream;
use Platformsh\Cli\Util\PortUtil;

class CurlStreamTest extends TestCase
{
    /** @var resource|null */
    private static $server;
    private static string $baseUrl;

    public static function setUpBeforeClass(): void
    {
        $port = PortUtil::getPort(18000);
        $router = \dirname(__DIR__) . '/data/curl-stream/router.php';
        $process = \proc_open([\PHP_BINARY, '-d', 'output_buffering=0', '-S', '127.0.0.1:' . $port, $router], [1 => ['file', '/dev/null', 'w'], 2 => ['file', '/dev/null', 'w']], $pipes);
        if ($process === false) {
            self::fail('Failed to start the test server');
        }
        self::$server = $process;
        self::$baseUrl = 'http://127.0.0.1:' . $port;
        $deadline = \microtime(true) + 5;
        while (!@\fsockopen('127.0.0.1', $port)) {
            if (\microtime(true) > $deadline) {
                self::fail('Timed out waiting for the test server');
            }
            \usleep(50000);
        }
    }

    public static function tearDownAfterClass(): void
    {
        if (self::$server !== null) {
            \proc_terminate(self::$server);
            \proc_close(self::$server);
            self::$server = null;
        }
    }

    public function testStream(): void
    {
        $stream = new CurlStream(self::$baseUrl . '/stream');
        $this->assertTrue($stream->waitForHeaders(5), $stream->getError());

        $content = '';
        $reads = 0;
        $deadline = \microtime(true) + 5;
        while (!$stream->eof() && \microtime(true) < $deadline) {
            $chunk = $stream->read(0.2);
            if ($chunk !== '') {
                $reads++;
                $content .= $chunk;
            }
        }
        $this->assertTrue($stream->eof());
        $this->assertSame("one\ntwo\nthree\n", $content);
        $this->assertGreaterThan(1, $reads, 'The body is read incrementally');
        $this->assertSame('', $stream->getError());
        $stream->close();
    }

    public function testRequestHeaders(): void
    {
        $stream = new CurlStream(self::$baseUrl . '/headers', [
            CURLOPT_HTTPHEADER => ['Authorization: Bearer abc'],
        ]);
        $this->assertTrue($stream->waitForHeaders(5), $stream->getError());
        $content = '';
        while (!$stream->eof()) {
            $content .= $stream->read(0.2);
        }
        $this->assertSame('Bearer abc', $content);
    }

    public function testNotFound(): void
    {
        $stream = new CurlStream(self::$baseUrl . '/missing');
        $this->assertFalse($stream->waitForHeaders(5));
        $this->assertSame('HTTP status 404', $stream->getError());
    }

    public function testTimeout(): void
    {
        $stream = new CurlStream(self::$baseUrl . '/slow');
        $start = \microtime(true);
        $this->assertFalse($stream->waitForHeaders(0.5));
        $this->assertLessThan(2, \microtime(true) - $start);
        $this->assertSame('Timed out waiting for a response', $stream->getError());
        $stream->close();
    }

    public function testConnectionError(): void
    {
        $port = PortUtil::getPort(19000);
        $stream = new CurlStream('http://127.0.0.1:' . $port . '/');
        $this->assertFalse($stream->waitForHeaders(5));
        $this->assertNotSame('', $stream->getError());
        $this->assertTrue($stream->eof());
    }
}
